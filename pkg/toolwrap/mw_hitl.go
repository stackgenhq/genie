package toolwrap

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/interrupt"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
	rtmemory "github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
)

// maxApprovalCacheSize limits the number of entries in the approval cache.
const maxApprovalCacheSize = 256

// defaultCacheTTL is the time-to-live for approval cache entries when no
// explicit TTL is configured. After this duration a previously approved
// tool+args combination requires fresh human approval.
const defaultCacheTTL = 10 * time.Minute

// approvalCache is a session-scoped, thread-safe cache of previously approved
// tool calls. It is owned by the Service and shared across all sub-agents so
// that a tool+args combination approved in one sub-agent is auto-approved in
// subsequent sub-agents within the same session. Entries expire after a
// configurable TTL to prevent stale approvals in long-running sessions.
type approvalCache struct {
	mu    sync.Mutex
	items map[string]time.Time
	order []string
	ttl   time.Duration
}

func newApprovalCache(ttl time.Duration) *approvalCache {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	return &approvalCache{items: make(map[string]time.Time), ttl: ttl}
}

func (c *approvalCache) has(key string) bool {
	c.mu.Lock()
	addedAt, ok := c.items[key]
	c.mu.Unlock()
	if !ok {
		return false
	}
	return time.Since(addedAt) < c.ttl
}

func (c *approvalCache) add(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[key]; exists {
		c.items[key] = time.Now()
		return
	}
	if len(c.order) >= maxApprovalCacheSize {
		evict := c.order[0]
		c.order = c.order[1:]
		delete(c.items, evict)
	}
	c.items[key] = time.Now()
	c.order = append(c.order, key)
}

// hitlApprovalMiddleware gates non-readonly tool calls on human approval.
// It shares a session-scoped approval cache (owned by the Service) and
// handles justification extraction, approval creation, and feedback storage.
type hitlApprovalMiddleware struct {
	store       hitl.ApprovalStore
	wm          *rtmemory.WorkingMemory
	blocking    bool // true = block in WaitForResolution; false = return interrupt.Error
	cache       *approvalCache
	approveList *ApproveList // optional in-memory temporary allowlist (blind or args-filter)
}

// HITLOption configures the behaviour of the HITL approval middleware.
type HITLOption func(*hitlApprovalMiddleware)

// WithNonBlockingHITL configures the middleware to return an
// [interrupt.Error] instead of blocking in [hitl.ApprovalStore.WaitForResolution].
//
// Use this when tool calls run inside an executor that models human
// interaction as an interrupt/resume cycle (e.g. Temporal workflows).
// The returned interrupt.Error carries the approval ID and the
// [hitl.ApprovalRequest] as its Payload.
//
// Default: blocking (the current in-process behaviour).
func WithNonBlockingHITL() HITLOption {
	return func(m *hitlApprovalMiddleware) { m.blocking = false }
}

// HITLApprovalMiddleware creates a new HITL approval middleware.
// Approval request events are emitted via the agui event bus (keyed by
// MessageOrigin in context), so no explicit event channel is needed.
//
// When called without WithSharedApprovalCache, a per-middleware cache is
// used (suitable for tests). The Service always passes a shared cache so
// that approvals carry across sub-agents within the same session.
func HITLApprovalMiddleware(
	store hitl.ApprovalStore,
	wm *rtmemory.WorkingMemory,
	opts ...HITLOption,
) Middleware {
	m := &hitlApprovalMiddleware{
		store:    store,
		wm:       wm,
		blocking: true,
	}
	for _, o := range opts {
		o(m)
	}
	if m.cache == nil {
		m.cache = newApprovalCache(defaultCacheTTL)
	}
	return m
}

// WithSharedApprovalCache injects a session-scoped approval cache shared
// across all sub-agents. When a tool+args combination is approved in one
// sub-agent, it is auto-approved in subsequent sub-agents within the
// same session, avoiding redundant HITL prompts.
func WithSharedApprovalCache(cache *approvalCache) HITLOption {
	return func(m *hitlApprovalMiddleware) { m.cache = cache }
}

// WithApproveListOption injects an in-memory approve list so the middleware can
// auto-approve when IsApproved returns true. Used by Service and by tests.
func WithApproveListOption(list *ApproveList) HITLOption {
	return func(m *hitlApprovalMiddleware) { m.approveList = list }
}

func (m *hitlApprovalMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		// Always extract _justification — the audit middleware needs it
		// even when HITL is disabled (store == nil).
		justification, strippedArgs := extractJustification(tc.Args)
		if justification != "" {
			tc.Args = strippedArgs
			tc.Justification = justification
		}

		ctx = toolcontext.WithJustification(ctx, justification)

		if m.store == nil {
			return next(ctx, tc)
		}

		logr := logger.GetLogger(ctx).With("fn", "HITLApprovalMiddleware", "tool", tc.ToolName)

		// Skip if tool is in allowlist.
		if m.store.IsAllowed(tc.ToolName) {
			return next(ctx, tc)
		}

		// Skip if tool+args match the in-memory approve list (blind or args filter).
		if m.approveList != nil && m.approveList.IsApproved(tc.ToolName, string(tc.Args)) {
			logr.Debug("HITL approve list hit — auto-approved (temporary allow)")
			return next(ctx, tc)
		}

		tid := m.effectiveThreadID(ctx)
		rid := m.effectiveRunID(ctx)

		// Session-scoped approval cache check — shared across all sub-agents.
		approvalKey := approvalFingerprint(tid, tc.ToolName, string(tc.Args))
		if m.cache.has(approvalKey) {
			logr.Debug("HITL cache hit — auto-approved (same session + tool + args)")
			return next(ctx, tc)
		}

		logr.Info("HITL approval gate entered",
			"threadID", tid, "runID", rid,
		)

		approval, err := m.store.Create(ctx, hitl.CreateRequest{
			ThreadID:      tid,
			RunID:         rid,
			ToolName:      tc.ToolName,
			Args:          string(tc.Args),
			SenderContext: messenger.MessageOriginFrom(ctx).String(),
			Question:      OriginalQuestionFrom(ctx),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create approval request for tool %s: %w", tc.ToolName, err)
		}

		m.emitApprovalRequest(ctx, approval.ID, tc.ToolName, string(tc.Args), tc.Justification)

		// Non-blocking mode: return an interrupt.Error so the executor can
		// model the wait as a Temporal signal or graph interrupt.
		if !m.blocking {
			return nil, &interrupt.Error{
				Kind:      interrupt.Approval,
				RequestID: approval.ID,
				Payload:   approval,
			}
		}

		logr.Info("waiting for human approval", "approval_id", approval.ID)
		resolved, err := m.store.WaitForResolution(ctx, approval.ID)
		if err != nil {
			return nil, fmt.Errorf("approval wait failed for tool %s: %w", tc.ToolName, err)
		}

		switch {
		case resolved.Status == hitl.StatusRejected && resolved.Feedback != "":
			m.storeFeedback(tc.ToolName, resolved.Feedback)
			logr.Info("tool call rejected with feedback", "feedback", resolved.Feedback)
			return nil, fmt.Errorf("tool call %s rejected by user: %s", tc.ToolName, resolved.Feedback)

		case resolved.Status == hitl.StatusRejected:
			logr.Info("tool call rejected by user")
			return nil, fmt.Errorf("tool call %s rejected by user", tc.ToolName)

		case resolved.Feedback != "":
			m.storeFeedback(tc.ToolName, resolved.Feedback)
			logr.Info("tool call approved with feedback — returning to LLM for re-planning",
				"feedback", resolved.Feedback)
			return nil, fmt.Errorf("tool call %s: user requested changes — %s — please adjust your approach and try again",
				tc.ToolName, resolved.Feedback)
		}

		logr.Info("tool call approved by user")
		m.cache.add(approvalKey)
		return next(ctx, tc)
	}
}

func (m *hitlApprovalMiddleware) effectiveThreadID(ctx context.Context) string {
	return agui.ThreadIDFromContext(ctx)
}

func (m *hitlApprovalMiddleware) effectiveRunID(ctx context.Context) string {
	return agui.RunIDFromContext(ctx)
}

func (m *hitlApprovalMiddleware) emitApprovalRequest(ctx context.Context, approvalID, toolName, args, justification string) {
	agui.Emit(ctx, agui.ToolApprovalRequestMsg{
		Type:          agui.EventToolApprovalRequest,
		ApprovalID:    approvalID,
		ToolName:      toolName,
		Arguments:     args,
		Justification: justification,
	})
}

func (m *hitlApprovalMiddleware) storeFeedback(toolName, feedback string) {
	if m.wm == nil || feedback == "" {
		return
	}
	key := fmt.Sprintf("hitl:feedback:%s", toolName)
	if existing, ok := m.wm.Recall(key); ok && existing != "" {
		m.wm.Store(key, existing+"\n"+feedback)
	} else {
		m.wm.Store(key, feedback)
	}
}

// approvalFingerprint produces a deterministic cache key for approval cache.
func approvalFingerprint(threadID, toolName, args string) string {
	h := sha256.New()
	h.Write([]byte(threadID))
	h.Write([]byte("|"))
	h.Write([]byte(toolName))
	h.Write([]byte("|"))
	h.Write([]byte(args))
	return fmt.Sprintf("%x", h.Sum(nil))
}
