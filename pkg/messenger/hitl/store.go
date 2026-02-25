package hitl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
)

// NotifierStore wraps an ApprovalStore to send notifications via Messenger.
// This struct exists to add a notification layer on top of the persistence layer,
// ensuring users are alerted when manual intervention is required.
// Without this wrapper, users would have to manually check the AG-UI or logs
// to know that the agent is waiting for approval.
type NotifierStore struct {
	realStore hitl.ApprovalStore
	messenger messenger.Messenger

	// pendingMu guards pendingApprovals and messageToApproval.
	pendingMu sync.Mutex
	// pendingApprovals maps "senderContext" -> FIFO queue of approval IDs.
	// Multiple tool calls can request HITL approval concurrently; each one
	// pushes its ID. GetPending returns the oldest; RemovePending removes a
	// specific ID once resolved.
	pendingApprovals map[string][]string
	// messageToApproval maps outgoing platform messageID -> approvalID.
	// When the user swipe-replies to an approval notification, we can
	// resolve the specific approval instead of relying on FIFO order.
	messageToApproval map[string]string
}

// NewNotifierStore creates a new NotifierStore.
// This function exists to instantiate the wrapper with its necessary dependencies.
// Without this function, callers would have to manually construct the struct, which
// is error-prone and exposes internal fields.
func NewNotifierStore(realStore hitl.ApprovalStore, m messenger.Messenger) *NotifierStore {
	return &NotifierStore{
		realStore:         realStore,
		messenger:         m,
		pendingApprovals:  make(map[string][]string),
		messageToApproval: make(map[string]string),
	}
}

// Create persists the approval and sends a notification if a valid sender context is present.
// This method exists to intercept the approval creation process and trigger a side-effect (notification).
// Without this method, the approval would be stored silently, and the user would not be notified
// via their chat platform.
func (s *NotifierStore) Create(ctx context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
	// 1. Create the approval in the real store
	approval, err := s.realStore.Create(ctx, req)
	if err != nil {
		return approval, err
	}

	// 2. Notify via messenger if we have a valid sender context
	origin := messenger.MessageOriginFrom(ctx)
	if !origin.IsZero() {
		if err := s.notifyMessenger(ctx, approval); err != nil {
			logger.GetLogger(ctx).Warn("failed to notify messenger", "error", err)
		}
		// Append to FIFO queue for this sender so concurrent approvals
		// are all tracked instead of overwriting each other.
		senderKey := origin.String()
		s.pendingMu.Lock()
		s.pendingApprovals[senderKey] = append(s.pendingApprovals[senderKey], approval.ID)
		logger.GetLogger(ctx).Info("pending approval queued",
			"senderCtx", senderKey,
			"approvalID", approval.ID,
			"queueLen", len(s.pendingApprovals[senderKey]),
		)
		s.pendingMu.Unlock()
	}

	return approval, nil
}

// Resolve delegates to the real store.
// This method exists to satisfy the ApprovalStore interface and pass through resolution requests.
// Without this method, NotifierStore relies on the embedded interface which might not behave as expected
// if not explicitly delegated.
func (s *NotifierStore) Resolve(ctx context.Context, req hitl.ResolveRequest) error {
	return s.realStore.Resolve(ctx, req)
}

// WaitForResolution delegates to the real store.
// This method exists to satisfy the ApprovalStore interface and block until approval/rejection.
// Without this method, the agent would not be able to wait for the user's decision.
func (s *NotifierStore) WaitForResolution(ctx context.Context, approvalID string) (hitl.ApprovalRequest, error) {
	return s.realStore.WaitForResolution(ctx, approvalID)
}

// Close delegates to the real store.
// This method exists to clean up resources held by the underlying store.
// Without this method, database connections or file handles might leak.
func (s *NotifierStore) Close() error {
	return s.realStore.Close()
}

// GetPending returns the oldest pending approval ID for the given sender context.
// Returns ("", false) when no approvals are queued.
func (s *NotifierStore) GetPending(ctx context.Context, senderContext string) (string, bool) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	q := s.pendingApprovals[senderContext]
	if len(q) == 0 {
		return "", false
	}
	logger.GetLogger(ctx).Info("GetPending returning oldest approval",
		"senderCtx", senderContext,
		"approvalID", q[0],
		"queueLen", len(q),
	)
	return q[0], true
}

// GetPendingByMessageID resolves the approval ID associated with an outgoing
// notification message. When a user swipe-replies to a specific approval
// notification, the incoming message contains the quoted message ID which
// maps to the approval via this method.
func (s *NotifierStore) GetPendingByMessageID(ctx context.Context, quotedMsgID string) (string, bool) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	approvalID, ok := s.messageToApproval[quotedMsgID]
	if ok {
		logger.GetLogger(ctx).Info("resolved approval via reply-to message",
			"quotedMsgID", quotedMsgID,
			"approvalID", approvalID,
		)
	}
	return approvalID, ok
}

// RemovePending removes the oldest approval from the sender's pending queue
// and cleans up any associated messageToApproval entries.
func (s *NotifierStore) RemovePending(ctx context.Context, senderContext string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	q := s.pendingApprovals[senderContext]
	if len(q) == 0 {
		return
	}
	// Clean up messageToApproval entries for the approval being removed.
	removedID := q[0]
	for msgID, aID := range s.messageToApproval {
		if aID == removedID {
			delete(s.messageToApproval, msgID)
		}
	}
	if len(q) <= 1 {
		delete(s.pendingApprovals, senderContext)
		logger.GetLogger(ctx).Info("pending approval queue emptied", "senderCtx", senderContext)
		return
	}
	// Pop oldest (FIFO)
	s.pendingApprovals[senderContext] = q[1:]
	logger.GetLogger(ctx).Info("pending approval dequeued",
		"senderCtx", senderContext,
		"remaining", len(q)-1,
	)
}

// RemovePendingByApprovalID removes a specific approval ID from the sender's
// queue (used when reply-to routing resolves a non-oldest approval).
func (s *NotifierStore) RemovePendingByApprovalID(ctx context.Context, senderContext, approvalID string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	// Clean up messageToApproval.
	for msgID, aID := range s.messageToApproval {
		if aID == approvalID {
			delete(s.messageToApproval, msgID)
		}
	}

	// Remove from queue.
	q := s.pendingApprovals[senderContext]
	filtered := make([]string, 0, len(q))
	for _, id := range q {
		if id != approvalID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		delete(s.pendingApprovals, senderContext)
	} else {
		s.pendingApprovals[senderContext] = filtered
	}
	logger.GetLogger(ctx).Info("pending approval removed by ID",
		"senderCtx", senderContext,
		"approvalID", approvalID,
		"remaining", len(filtered),
	)
}

func (s *NotifierStore) notifyMessenger(ctx context.Context, approval hitl.ApprovalRequest) error {
	// Prefer structured MessageOrigin for channel resolution.
	var channelID string
	origin := messenger.MessageOriginFrom(ctx)
	if origin.IsZero() {
		return fmt.Errorf("no originating channel context available")
	}
	channelID = origin.Channel.ID

	// Pretty-print JSON args for readability.
	prettyArgs := approval.Args
	if json.Valid([]byte(approval.Args)) {
		var buf bytes.Buffer
		if err := json.Indent(&buf, []byte(approval.Args), "", "  "); err == nil {
			prettyArgs = buf.String()
		}
	}

	sendReq := messenger.SendRequest{
		Channel: messenger.Channel{ID: channelID},
	}

	// All platforms get plaintext content as a baseline.
	sendReq.Content = messenger.MessageContent{Text: approval.String()}

	// Let the adapter format the approval with platform-native constructs
	// (Slack blocks, Google Chat cards, etc.). Text-only adapters return req unchanged.
	sendReq = s.messenger.FormatApproval(sendReq, messenger.ApprovalInfo{
		ID:       approval.ID,
		ToolName: approval.ToolName,
		Args:     prettyArgs,
		Feedback: approval.Feedback,
	})

	resp, err := s.messenger.Send(ctx, sendReq)
	if err != nil {
		// Best-effort notification; log failure but don't fail the underlying operation.
		logger.GetLogger(ctx).Warn("failed to send hitl notification", "error", err)
	} else if resp.MessageID != "" {
		// Track which outgoing message corresponds to this approval so
		// swipe-replies can resolve the correct approval.
		s.pendingMu.Lock()
		s.messageToApproval[resp.MessageID] = approval.ID
		s.pendingMu.Unlock()
	}
	return err
}

func (s *NotifierStore) IsAllowed(toolName string) bool {
	return s.realStore.IsAllowed(toolName)
}

// RecoverPending delegates to the real store.
func (s *NotifierStore) RecoverPending(ctx context.Context, maxAge time.Duration) (hitl.RecoverResult, error) {
	return s.realStore.RecoverPending(ctx, maxAge)
}

// ExpireStale delegates to the real store to mark pending approvals past their
// deadline as expired. Without this delegation the background reaper goroutine
// would bypass the NotifierStore wrapper and stale approvals would never be
// cleaned up.
func (s *NotifierStore) ExpireStale(ctx context.Context) (int64, error) {
	return s.realStore.ExpireStale(ctx)
}

// ListPending delegates to the real store to return all currently pending
// approval requests. Without this delegation the GUILD API would be unable to
// discover which tool calls are awaiting human approval when the NotifierStore
// wrapper is in use.
func (s *NotifierStore) ListPending(ctx context.Context) ([]hitl.ApprovalRequest, error) {
	return s.realStore.ListPending(ctx)
}
