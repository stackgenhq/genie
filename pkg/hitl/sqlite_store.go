package hitl

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/stackgenhq/genie/pkg/db"
	"github.com/stackgenhq/genie/pkg/retrier"
	"gorm.io/gorm"
)

const allTools = "*"

type Config struct {
	AlwaysAllowed []string `yaml:"always_allowed" toml:"always_allowed" json:"always_allowed"`
	DeniedTools   []string `yaml:"denied_tools" toml:"denied_tools" json:"denied_tools"`
}

func DefaultConfig() Config {
	return Config{
		AlwaysAllowed: []string{},
		DeniedTools:   []string{},
	}
}

func (c Config) readOnlyTools() []string {
	for _, tool := range c.AlwaysAllowed {
		if tool == allTools {
			return []string{allTools}
		}
	}
	return append(defaultReadOnlyTools, c.AlwaysAllowed...)
}

// IsAllowed checks if a tool is exempt from HITL approval.
// Supports exact names (case-insensitive) and prefix wildcards:
//   - "*" matches all tools (HITL fully disabled)
//   - "browser_*" matches browser_navigate, browser_read_text, etc.
//   - "read_file" matches only read_file (exact)
func (c Config) IsAllowed(toolName string) bool {
	lower := strings.ToLower(toolName)
	for _, pattern := range c.readOnlyTools() {
		if pattern == allTools {
			return true
		}
		lp := strings.ToLower(pattern)
		if strings.HasSuffix(lp, "*") {
			prefix := lp[:len(lp)-1]
			if strings.HasPrefix(lower, prefix) {
				return true
			}
			continue
		}
		if lp == lower {
			return true
		}
	}
	return false
}

// IsDenied checks if a tool is explicitly denied via the denied_tools config.
// Supports exact names (case-insensitive) and prefix wildcards, same as IsAllowed.
func (c Config) IsDenied(toolName string) bool {
	lower := strings.ToLower(toolName)
	for _, pattern := range c.DeniedTools {
		lp := strings.ToLower(pattern)
		if lp == allTools {
			return true
		}
		if strings.HasSuffix(lp, "*") {
			prefix := lp[:len(lp)-1]
			if strings.HasPrefix(lower, prefix) {
				return true
			}
			continue
		}
		if lp == lower {
			return true
		}
	}
	return false
}

// gormStore implements ApprovalStore backed by GORM.
// Pending approvals are tracked with in-process channels for synchronous
// wait/notify between the ToolWrapper goroutine and the HTTP handler.
// The GORM layer provides durable persistence so approval history
// survives restarts.
type gormStore struct {
	db      *gorm.DB
	waiters sync.Map // map[approvalID]chan struct{}
	cfg     Config
}

// NewStore creates an ApprovalStore backed by the given GORM database.
// The caller is responsible for opening and migrating the database (via pkg/db).
// This constructor does NOT own the database lifecycle — Close() is a no-op.
func (c Config) NewStore(gormDB *gorm.DB) ApprovalStore {
	return &gormStore{
		db:  gormDB,
		cfg: c,
	}
}

func (s *gormStore) IsAllowed(toolName string) bool {
	return s.cfg.IsAllowed(toolName)
}

// Create persists a new pending approval request and returns it.
// A UUID is generated as the approval ID.
func (s *gormStore) Create(ctx context.Context, req CreateRequest) (ApprovalRequest, error) {
	now := time.Now().UTC()
	row := db.Approval{
		ID:            uuid.NewString(),
		ThreadID:      req.ThreadID,
		RunID:         req.RunID,
		ToolName:      req.ToolName,
		Args:          req.Args,
		Status:        string(StatusPending),
		CreatedAt:     now,
		SenderContext: req.SenderContext,
		Question:      req.Question,
	}

	if err := retrier.Retry(ctx, func() error {
		return s.db.WithContext(ctx).Create(&row).Error
	}); err != nil {
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
			"feedback":    req.Feedback,
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
// It uses a hybrid approach: primarily the in-process channel for instant
// notification, with a DB polling fallback every 5s. The polling fallback
// ensures resolution is detected even if the waiter channel was lost (e.g.
// after a server restart where RecoverPending re-registered the channel but
// the Resolve call arrived before the channel was set up).
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

	// Hybrid wait: channel notification + DB polling fallback.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ch:
			// Channel signalled — re-read from DB to get resolved state.
			return s.get(ctx, approvalID)
		case <-ticker.C:
			// Polling fallback — detect resolution even without channel signal.
			a, pollErr := s.get(ctx, approvalID)
			if pollErr != nil {
				return ApprovalRequest{}, pollErr
			}
			if a.Status != StatusPending {
				return a, nil
			}
		case <-ctx.Done():
			return ApprovalRequest{}, ctx.Err()
		}
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
		ID:            row.ID,
		ThreadID:      row.ThreadID,
		RunID:         row.RunID,
		ToolName:      row.ToolName,
		Args:          row.Args,
		Status:        ApprovalStatus(row.Status),
		Feedback:      row.Feedback,
		CreatedAt:     row.CreatedAt,
		ResolvedAt:    row.ResolvedAt,
		ResolvedBy:    row.ResolvedBy,
		SenderContext: row.SenderContext,
		Question:      row.Question,
	}
}

func (s *gormStore) ReadOnlyTools() []string {
	return s.cfg.readOnlyTools()
}

// ListPending returns all approval requests currently in "pending" state.
// This is used by external HTTP APIs to surface which tool calls are
// awaiting human approval. Without this, operators would need direct
// database access to discover pending approvals.
func (s *gormStore) ListPending(ctx context.Context) ([]ApprovalRequest, error) {
	var rows []db.Approval
	if err := s.db.WithContext(ctx).
		Where("status = ?", string(StatusPending)).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to query pending approvals: %w", err)
	}
	result := make([]ApprovalRequest, 0, len(rows))
	for _, row := range rows {
		result = append(result, toApprovalRequest(row))
	}
	return result, nil
}

// RecoverPending handles approvals left in "pending" state from a previous
// server instance. Approvals older than maxAge are marked as "expired";
// more recent ones get fresh waiter channels registered so they can still
// be resolved via the HTTP API.
func (s *gormStore) RecoverPending(ctx context.Context, maxAge time.Duration) (RecoverResult, error) {
	var pending []db.Approval
	if err := s.db.WithContext(ctx).
		Where("status = ?", string(StatusPending)).
		Find(&pending).Error; err != nil {
		return RecoverResult{}, fmt.Errorf("failed to query pending approvals: %w", err)
	}

	if len(pending) == 0 {
		return RecoverResult{}, nil
	}

	cutoff := time.Now().UTC().Add(-maxAge)
	now := time.Now().UTC()
	var result RecoverResult

	for _, row := range pending {
		if row.CreatedAt.Before(cutoff) {
			// Too old — expire it.
			s.db.WithContext(ctx).
				Model(&db.Approval{}).
				Where("id = ? AND status = ?", row.ID, StatusPending).
				Updates(map[string]interface{}{
					"status":      StatusExpired,
					"resolved_at": now,
					"resolved_by": "system:startup-recovery",
					"feedback":    "Expired: server restarted while approval was pending",
				})
			result.Expired++
		} else {
			// Recent — re-register waiter channel so it can still be resolved.
			s.waiters.LoadOrStore(row.ID, make(chan struct{}))
			result.Recovered++

			// If the original user question was saved, mark this approval
			// as replayable so the bootstrap can spawn a waiter goroutine.
			if row.Question != "" {
				result.Replayable = append(result.Replayable, ReplayableApproval{
					ApprovalID:    row.ID,
					Question:      row.Question,
					SenderContext: row.SenderContext,
				})
			}
		}
	}

	return result, nil
}
