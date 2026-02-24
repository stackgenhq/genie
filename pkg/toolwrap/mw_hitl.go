package toolwrap

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/interrupt"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
)

// maxApprovalCacheSize limits the number of entries in the approval cache.
const maxApprovalCacheSize = 256

// hitlApprovalMiddleware gates non-readonly tool calls on human approval.
// It owns the session-scoped approval cache and handles justification
// extraction, approval creation, and feedback storage.
type hitlApprovalMiddleware struct {
	store    hitl.ApprovalStore
	wm       *rtmemory.WorkingMemory
	blocking bool // true = block in WaitForResolution; false = return interrupt.Error

	approvalMu    sync.Mutex
	approvalCache map[string]struct{}
	approvalOrder []string
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
func HITLApprovalMiddleware(
	store hitl.ApprovalStore,
	wm *rtmemory.WorkingMemory,
	opts ...HITLOption,
) Middleware {
	m := &hitlApprovalMiddleware{
		store:         store,
		wm:            wm,
		blocking:      true,
		approvalCache: make(map[string]struct{}),
	}
	for _, o := range opts {
		o(m)
	}
	return m
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

		if m.store == nil {
			return next(ctx, tc)
		}

		logr := logger.GetLogger(ctx).With("fn", "HITLApprovalMiddleware", "tool", tc.ToolName)

		// Skip if tool is in allowlist.
		if m.store.IsAllowed(tc.ToolName) {
			return next(ctx, tc)
		}

		tid := m.effectiveThreadID(ctx)
		rid := m.effectiveRunID(ctx)

		// Session-scoped approval cache check.
		approvalKey := approvalFingerprint(tid, tc.ToolName, string(tc.Args))
		m.approvalMu.Lock()
		_, cached := m.approvalCache[approvalKey]
		m.approvalMu.Unlock()
		if cached {
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
		m.storeApproval(approvalKey)
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

func (m *hitlApprovalMiddleware) storeApproval(key string) {
	m.approvalMu.Lock()
	defer m.approvalMu.Unlock()
	if _, exists := m.approvalCache[key]; exists {
		return
	}
	if len(m.approvalOrder) >= maxApprovalCacheSize {
		evict := m.approvalOrder[0]
		m.approvalOrder = m.approvalOrder[1:]
		delete(m.approvalCache, evict)
	}
	m.approvalCache[key] = struct{}{}
	m.approvalOrder = append(m.approvalOrder, key)
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
