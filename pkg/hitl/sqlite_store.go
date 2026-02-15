package hitl

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/db"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// gormStore implements ApprovalStore backed by GORM.
// Pending approvals are tracked with in-process channels for synchronous
// wait/notify between the ToolWrapper goroutine and the HTTP handler.
// The GORM layer provides durable persistence so approval history
// survives restarts.
type gormStore struct {
	db      *gorm.DB
	waiters sync.Map // map[approvalID]chan struct{}
}

// NewStore creates an ApprovalStore backed by the given GORM database.
// The caller is responsible for opening and migrating the database (via pkg/db).
// This constructor does NOT own the database lifecycle — Close() is a no-op.
func NewStore(gormDB *gorm.DB) ApprovalStore {
	return &gormStore{db: gormDB}
}

// Create persists a new pending approval request and returns it.
// A UUID is generated as the approval ID.
func (s *gormStore) Create(ctx context.Context, req CreateRequest) (ApprovalRequest, error) {
	now := time.Now().UTC()
	row := db.Approval{
		ID:        uuid.NewString(),
		ThreadID:  req.ThreadID,
		RunID:     req.RunID,
		ToolName:  req.ToolName,
		Args:      req.Args,
		Status:    string(StatusPending),
		CreatedAt: now,
	}

	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return ApprovalRequest{}, fmt.Errorf("failed to insert approval: %w", err)
	}

	// Pre-create the waiter channel so WaitForResolution can select on it.
	ch := make(chan struct{})
	s.waiters.Store(row.ID, ch)

	return toApprovalRequest(row), nil
}

// Resolve updates an approval to approved or rejected and unblocks any waiting goroutine.
func (s *gormStore) Resolve(ctx context.Context, req ResolveRequest) error {
	if req.Decision != StatusApproved && req.Decision != StatusRejected {
		return fmt.Errorf("invalid decision %q: must be %q or %q", req.Decision, StatusApproved, StatusRejected)
	}

	now := time.Now().UTC()
	result := s.db.WithContext(ctx).
		Model(&db.Approval{}).
		Where("id = ? AND status = ?", req.ApprovalID, string(StatusPending)).
		Updates(map[string]interface{}{
			"status":      string(req.Decision),
			"resolved_at": now,
			"resolved_by": req.ResolvedBy,
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update approval: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("approval %q not found or already resolved", req.ApprovalID)
	}

	// Signal the waiting goroutine.
	if ch, ok := s.waiters.LoadAndDelete(req.ApprovalID); ok {
		close(ch.(chan struct{}))
	}

	return nil
}

// WaitForResolution blocks until the approval is resolved or ctx is cancelled.
// It first checks the DB (in case it was resolved before we started waiting),
// then waits on the in-process channel.
func (s *gormStore) WaitForResolution(ctx context.Context, approvalID string) (ApprovalRequest, error) {
	// Fast path: check if already resolved.
	approval, err := s.get(ctx, approvalID)
	if err != nil {
		return ApprovalRequest{}, err
	}
	if approval.Status != StatusPending {
		return approval, nil
	}

	// Load or create the waiter channel.
	chRaw, _ := s.waiters.LoadOrStore(approvalID, make(chan struct{}))
	ch := chRaw.(chan struct{})

	// Wait for signal or context cancellation.
	select {
	case <-ch:
		// Re-read from DB to get the resolved state.
		return s.get(ctx, approvalID)
	case <-ctx.Done():
		return ApprovalRequest{}, ctx.Err()
	}
}

// get reads a single approval by ID.
func (s *gormStore) get(ctx context.Context, approvalID string) (ApprovalRequest, error) {
	var row db.Approval
	if err := s.db.WithContext(ctx).Where("id = ?", approvalID).First(&row).Error; err != nil {
		return ApprovalRequest{}, fmt.Errorf("failed to read approval %q: %w", approvalID, err)
	}
	return toApprovalRequest(row), nil
}

// Close is a no-op for gormStore — the DB lifecycle is owned by pkg/db.
func (s *gormStore) Close() error {
	// Signal any lingering waiters so they don't block forever.
	s.waiters.Range(func(key, value any) bool {
		ch := value.(chan struct{})
		select {
		case <-ch: // already closed
		default:
			close(ch)
		}
		s.waiters.Delete(key)
		return true
	})
	return nil
}

// toApprovalRequest converts a db.Approval GORM model to the hitl.ApprovalRequest
// domain type.
func toApprovalRequest(row db.Approval) ApprovalRequest {
	return ApprovalRequest{
		ID:         row.ID,
		ThreadID:   row.ThreadID,
		RunID:      row.RunID,
		ToolName:   row.ToolName,
		Args:       row.Args,
		Status:     ApprovalStatus(row.Status),
		CreatedAt:  row.CreatedAt,
		ResolvedAt: row.ResolvedAt,
		ResolvedBy: row.ResolvedBy,
	}
}
