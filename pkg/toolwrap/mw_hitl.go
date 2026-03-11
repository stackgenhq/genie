// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/interrupt"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/orchestrator/orchestratorcontext"
	rtmemory "github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/security/authcontext"
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
	store              hitl.ApprovalStore
	wm                 *rtmemory.WorkingMemory
	blocking           bool // true = block in WaitForResolution; false = return interrupt.Error
	cache              *approvalCache
	approveList        *ApproveList       // optional in-memory temporary allowlist (blind or args-filter)
	auditor            audit.Auditor      // optional durable auditor for HITL decisions
	backgroundBehavior BackgroundBehavior // policy for cron triggers / internal tasks
}

// HITLOption configures the behaviour of the HITL approval middleware.
type HITLOption func(*hitlApprovalMiddleware)

// BackgroundBehavior defines the policy for handling tool calls in background tasks (e.g. cron).
type BackgroundBehavior int

const (
	BackgroundBehaviorReject BackgroundBehavior = iota
	BackgroundBehaviorApprove
	BackgroundBehaviorBlock
)

// ParseBackgroundBehavior converts a string to a BackgroundBehavior.
func ParseBackgroundBehavior(s string) BackgroundBehavior {
	switch s {
	case "approve":
		return BackgroundBehaviorApprove
	case "block":
		return BackgroundBehaviorBlock
	default:
		return BackgroundBehaviorReject
	}
}

func (b BackgroundBehavior) String() string {
	switch b {
	case BackgroundBehaviorApprove:
		return "approve"
	case BackgroundBehaviorBlock:
		return "block"
	default:
		return "reject"
	}
}

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

// WithHITLBackgroundBehavior configures how the middleware handles tool calls
// when they originate from an internal background task (e.g. cron triggers).
func WithHITLBackgroundBehavior(b BackgroundBehavior) HITLOption {
	return func(m *hitlApprovalMiddleware) {
		m.backgroundBehavior = b
	}
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

// WithHITLAuditor injects an auditor so HITL approval/rejection decisions
// are written to the durable audit trail (not just the AG-UI event bus).
func WithHITLAuditor(a audit.Auditor) HITLOption {
	return func(m *hitlApprovalMiddleware) { m.auditor = a }
}

func (m *hitlApprovalMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		// Always extract _justification — the audit middleware needs it
		// even when HITL is disabled (store == nil).
		justification, strippedArgs, found := extractJustification(tc.Args)
		if found {
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
			m.emitAutoApproved(ctx, tc.ToolName, string(tc.Args), tc.Justification)
			m.auditHITLDecision(ctx, tc.ToolName, string(tc.Args), tc.Justification, "auto_approved", "always_allowed", "")
			return next(ctx, tc)
		}

		// Skip if tool+args match the in-memory approve list (blind or args filter).
		if m.approveList != nil && m.approveList.IsApproved(tc.ToolName, string(tc.Args)) {
			logr.Debug("HITL approve list hit — auto-approved (temporary allow)")
			m.emitAutoApproved(ctx, tc.ToolName, string(tc.Args), tc.Justification)
			m.auditHITLDecision(ctx, tc.ToolName, string(tc.Args), tc.Justification, "auto_approved", "approve_list", "")
			return next(ctx, tc)
		}

		tid := m.effectiveThreadID(ctx)
		rid := m.effectiveRunID(ctx)

		// Session-scoped approval cache check — shared across all sub-agents.
		approvalKey := approvalFingerprint(tid, tc.ToolName, string(tc.Args))
		if m.cache.has(approvalKey) {
			logr.Debug("HITL cache hit — auto-approved (same session + tool + args)")
			m.emitAutoApproved(ctx, tc.ToolName, string(tc.Args), tc.Justification)
			m.auditHITLDecision(ctx, tc.ToolName, string(tc.Args), tc.Justification, "auto_approved", "cache_hit", "")
			return next(ctx, tc)
		}

		// Check if this is a background task (e.g. cron trigger).
		// If so, there is no user attached to the session to approve it.
		// Handle according to the backgroundBehavior configuration.
		if orchestratorcontext.IsInternalTask(ctx) {
			handled, res, err := m.handleBackgroundTask(ctx, tc, next)
			if handled {
				return res, err
			}
		}

		logr.Info("HITL approval gate entered",
			"threadID", tid, "runID", rid,
		)

		approval, err := m.store.Create(ctx, hitl.CreateRequest{
			ThreadID:      tid,
			RunID:         rid,
			ToolName:      tc.ToolName,
			Args:          string(tc.Args),
			CreatedBy:     authcontext.GetPrincipal(ctx).ID,
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
			m.auditHITLDecision(ctx, tc.ToolName, string(tc.Args), tc.Justification, "rejected", "", resolved.Feedback)
			return nil, fmt.Errorf("%w: tool call %s rejected by user: %s", ErrToolCallRejected, tc.ToolName, resolved.Feedback)

		case resolved.Status == hitl.StatusRejected:
			logr.Info("tool call rejected by user")
			m.auditHITLDecision(ctx, tc.ToolName, string(tc.Args), tc.Justification, "rejected", "", "")
			return nil, fmt.Errorf("%w: tool call %s rejected by user", ErrToolCallRejected, tc.ToolName)

		case resolved.Feedback != "":
			m.storeFeedback(tc.ToolName, resolved.Feedback)
			logr.Info("tool call approved with feedback — returning to LLM for re-planning",
				"feedback", resolved.Feedback)
			return nil, fmt.Errorf("%w: tool call %s: user requested changes — %s — please adjust your approach and try again",
				ErrToolCallRejected, tc.ToolName, resolved.Feedback)
		}

		logr.Info("tool call approved by user")
		m.cache.add(approvalKey)
		m.auditHITLDecision(ctx, tc.ToolName, string(tc.Args), tc.Justification, "approved", "", "")
		return next(ctx, tc)
	}
}

// handleBackgroundTask evaluates the configured background behavior when a tool call
// originates from an internal task. It returns handled=true if it chose a final action
// (auto-approve or reject) and handled=false if it should block and emit a normal approval request.
func (m *hitlApprovalMiddleware) handleBackgroundTask(ctx context.Context, tc *ToolCallContext, next Handler) (bool, any, error) {
	logr := logger.GetLogger(ctx).With("fn", "HITLApprovalMiddleware", "tool", tc.ToolName)

	switch m.backgroundBehavior {
	case BackgroundBehaviorApprove:
		logr.Info("background task tool call auto-approved via config")
		m.emitAutoApproved(ctx, tc.ToolName, string(tc.Args), tc.Justification)
		m.auditHITLDecision(ctx, tc.ToolName, string(tc.Args), tc.Justification, "auto_approved", "background_task", "")
		res, err := next(ctx, tc)
		return true, res, err
	case BackgroundBehaviorBlock:
		logr.Info("background task tool call blocked awaiting approval via config")
		// Fall through to normal HITL approval flow
		return false, nil, nil
	default: // "reject" (or any unrecognized value)
		logr.Warn("background task requested an unapproved tool call, rejecting via config", "HITLBackgroundBehavior", m.backgroundBehavior)
		m.auditHITLDecision(ctx, tc.ToolName, string(tc.Args), tc.Justification, "rejected", "background_task", "")
		return true, nil, fmt.Errorf("%w: requires human approval, which is not supported in background tasks (HITLBackgroundBehavior=%v)", ErrToolCallRejected, m.backgroundBehavior)
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

func (m *hitlApprovalMiddleware) emitAutoApproved(ctx context.Context, toolName, args, justification string) {
	agui.Emit(ctx, agui.ToolApprovalRequestMsg{
		Type:          agui.EventToolApprovalRequest,
		ToolName:      toolName,
		Arguments:     args,
		Justification: justification,
		AutoApproved:  true,
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

// AuditEventHITLDecision is the audit event type for HITL approval/rejection.
const AuditEventHITLDecision audit.EventType = "hitl_decision"

// auditHITLDecision logs an HITL approval or rejection decision to the
// durable audit trail. The decision field is "approved", "rejected",
// or "auto_approved".
//
// reason is reserved for auto-approval classifications (e.g. "always_allowed",
// "approve_list", "cache_hit"). feedback carries user-provided text from
// rejection-with-feedback flows. This separation avoids ambiguity in audit
// records between automated reasons and human input.
func (m *hitlApprovalMiddleware) auditHITLDecision(ctx context.Context, toolName, args, justification, decision, reason, feedback string) {
	if m.auditor == nil {
		return
	}
	metadata := map[string]any{
		"tool":          toolName,
		"decision":      decision,
		"justification": justification,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}
	if reason != "" {
		metadata["reason"] = reason
	}
	if feedback != "" {
		metadata["feedback"] = feedback
	}
	if len(args) > 0 {
		metadata["args"] = TruncateForAudit(args, 512)
	}
	m.auditor.Log(ctx, audit.LogRequest{
		EventType: AuditEventHITLDecision,
		Actor:     "hitl",
		Action:    "tool_" + decision,
		Metadata:  metadata,
	})
}
