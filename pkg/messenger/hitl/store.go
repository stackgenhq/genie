package hitl

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
)

// NotifierStore wraps an ApprovalStore to send notifications via Messenger.
// This struct exists to add a notification layer on top of the persistence layer,
// ensuring users are alerted when manual intervention is required.
// Without this wrapper, users would have to manually check the AG-UI or logs
// to know that the agent is waiting for approval.
type NotifierStore struct {
	realStore hitl.ApprovalStore
	messenger messenger.Messenger

	// pendingApprovals maps "senderContext" -> "approvalID"
	// This allows the messenger handler to look up the approval ID when the user replies "Yes".
	pendingApprovals sync.Map
}

// NewNotifierStore creates a new NotifierStore.
// This function exists to instantiate the wrapper with its necessary dependencies.
// Without this function, callers would have to manually construct the struct, which
// is error-prone and exposes internal fields.
func NewNotifierStore(realStore hitl.ApprovalStore, m messenger.Messenger) *NotifierStore {
	return &NotifierStore{
		realStore: realStore,
		messenger: m,
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
	senderCtx := messenger.SenderContextFrom(ctx)
	if senderCtx != "" {
		s.notifyMessenger(ctx, senderCtx, approval)
		// Store mapping for easy lookup on reply
		s.pendingApprovals.Store(senderCtx, approval.ID)
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

// GetPending returns the approval ID pending for the given sender context, if any.
// This method exists to allow external callers (like the chat handler) to look up
// which approval request corresponds to a "Yes"/"No" reply from a specific user/channel.
// Without this method, we cannot correlate a conversational reply to a specific approval ID.
func (s *NotifierStore) GetPending(senderContext string) (string, bool) {
	val, ok := s.pendingApprovals.Load(senderContext)
	if !ok {
		return "", false
	}
	return val.(string), true
}

// RemovePending removes the pending approval for the sender context.
// This method exists to clean up the pending state after an approval has been resolved.
// Without this method, the map would grow indefinitely and might cause incorrect resolutions
// for future interactions.
func (s *NotifierStore) RemovePending(senderContext string) {
	s.pendingApprovals.Delete(senderContext)
}

func (s *NotifierStore) notifyMessenger(ctx context.Context, senderCtx string, approval hitl.ApprovalRequest) {
	// senderCtx format: "platform:senderID:channelID"
	parts := strings.SplitN(senderCtx, ":", 3)
	if len(parts) < 3 {
		// Invalid or non-messenger context (e.g. "tui:local")
		return
	}

	channelID := parts[2]

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

	_, err := s.messenger.Send(ctx, sendReq)
	if err != nil {
		// Best-effort notification; log failure but don't fail the underlying operation.
		logger.GetLogger(ctx).Warn("failed to send hitl notification", "error", err)
	}
}

func (s *NotifierStore) IsAllowed(toolName string) bool {
	return s.realStore.IsAllowed(toolName)
}

// RecoverPending delegates to the real store.
func (s *NotifierStore) RecoverPending(ctx context.Context, maxAge time.Duration) (hitl.RecoverResult, error) {
	return s.realStore.RecoverPending(ctx, maxAge)
}
