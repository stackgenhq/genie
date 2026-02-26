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
	AlwaysAllowed []string      `yaml:"always_allowed,omitempty" toml:"always_allowed,omitempty" json:"always_allowed"`
	DeniedTools   []string      `yaml:"denied_tools,omitempty" toml:"denied_tools,omitempty" json:"denied_tools"`
	ApprovalTTL   time.Duration `yaml:"approval_ttl,omitempty" toml:"approval_ttl,omitempty" json:"approval_ttl"`
	CacheTTL      time.Duration `yaml:"cache_ttl,omitempty" toml:"cache_ttl,omitempty" json:"cache_ttl"`
}

// DefaultConfig returns sensible defaults.
// ApprovalTTL defaults to 30 minutes — pending approvals older than this
// are automatically expired by the background reaper.
// CacheTTL defaults to 10 minutes — approved tool+args combinations are
// auto-approved for this duration before requiring fresh human approval.
func DefaultConfig() Config {
	return Config{
		AlwaysAllowed: []string{},
		DeniedTools:   []string{},
		ApprovalTTL:   30 * time.Minute,
		CacheTTL:      10 * time.Minute,
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
// writeMu serializes all writes (Create, Resolve, ExpireStale, RecoverPending)
// so that only one writer hits SQLite at a time, avoiding SQLITE_BUSY when
// many tool calls request approval concurrently. Reads (get, ListPending) do
// not take the lock so they can run concurrently with writes; SQLite WAL
// allows multiple readers and one writer.
type gormStore struct {
	db      *gorm.DB
	waiters sync.Map // map[approvalID]chan struct{}
	cfg     Config
	writeMu sync.RWMutex
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
// A UUID is generated as the approval ID. If ApprovalTTL is configured,
// an ExpiresAt timestamp is set; the background reaper will expire
// approvals that exceed this deadline.
// Writes are serialized via writeMu to avoid SQLITE_BUSY under concurrent tool calls.
func (s *gormStore) Create(ctx context.Context, req CreateRequest) (ApprovalRequest, error) {
	now := time.Now().UTC()
	row := db.Approval{
		ID:            uuid.NewString(),
		ThreadID:      req.ThreadID,
		RunID:         req.RunID,
		TenantID:      req.TenantID,
		ToolName:      req.ToolName,
		Args:          req.Args,
		Status:        string(StatusPending),
		CreatedAt:     now,
		SenderContext: req.SenderContext,
		Question:      req.Question,
	}
	if s.cfg.ApprovalTTL > 0 {
		exp := now.Add(s.cfg.ApprovalTTL)
		row.ExpiresAt = &exp
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
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

// Resolve updates an approval to approved or rejected and unblocks any
// waiting goroutine. Uses SELECT ... FOR UPDATE to prevent concurrent
// resolvers from racing on the same row.
// Writes are serialized via writeMu to avoid SQLITE_BUSY when other
// goroutines are creating approvals or running the reaper.
func (s *gormStore) Resolve(ctx context.Context, req ResolveRequest) error {
	if req.Decision != StatusApproved && req.Decision != StatusRejected {
		return fmt.Errorf("invalid decision %q: must be %q or %q", req.Decision, StatusApproved, StatusRejected)
	}

	now := time.Now().UTC()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row db.Approval
		if lockErr := tx.
			Where("id = ? AND status = ?", req.ApprovalID, string(StatusPending)).
			Set("gorm:query_option", "FOR UPDATE").
			First(&row).Error; lockErr != nil {
			return fmt.Errorf("approval %q not found or already resolved", req.ApprovalID)
		}
		return tx.Model(&row).Updates(map[string]interface{}{
			"status":      string(req.Decision),
			"resolved_at": now,
			"resolved_by": req.ResolvedBy,
			"feedback":    req.Feedback,
		}).Error
	})
	if err != nil {
		return err
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
		TenantID:      row.TenantID,
		ToolName:      row.ToolName,
		Args:          row.Args,
		Status:        ApprovalStatus(row.Status),
		Feedback:      row.Feedback,
		CreatedAt:     row.CreatedAt,
		ExpiresAt:     row.ExpiresAt,
		ResolvedAt:    row.ResolvedAt,
		ResolvedBy:    row.ResolvedBy,
		SenderContext: row.SenderContext,
		Question:      row.Question,
	}
}

func (s *gormStore) ReadOnlyTools() []string {
	return s.cfg.readOnlyTools()
}

// ListPending returns all approval requests currently in "pending" state
// whose deadline (expires_at) has not yet passed.
// This is used by external HTTP APIs to surface which tool calls are
// awaiting human approval. Without this, operators would need direct
// database access to discover pending approvals.
func (s *gormStore) ListPending(ctx context.Context) ([]ApprovalRequest, error) {
	now := time.Now().UTC()
	var rows []db.Approval
	if err := s.db.WithContext(ctx).
		Where("status = ? AND (expires_at IS NULL OR expires_at > ?)", string(StatusPending), now).
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

// ExpireStale marks all pending approvals whose expires_at has passed as
// expired. It returns the number of rows affected. This is meant to be
// called periodically by a background reaper goroutine.
// Writes are serialized via writeMu to avoid SQLITE_BUSY with Create/Resolve.
func (s *gormStore) ExpireStale(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	result := s.db.WithContext(ctx).
		Model(&db.Approval{}).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at <= ?", string(StatusPending), now).
		Updates(map[string]interface{}{
			"status":      string(StatusExpired),
			"resolved_at": now,
			"resolved_by": "system:ttl-reaper",
			"feedback":    "Expired: approval TTL exceeded",
		})
	if result.Error != nil {
		return 0, fmt.Errorf("expire stale approvals: %w", result.Error)
	}
	// Unblock any waiters for expired approvals.
	if result.RowsAffected > 0 {
		var expired []db.Approval
		s.db.WithContext(ctx).
			Where("status = ? AND resolved_by = ?", string(StatusExpired), "system:ttl-reaper").
			Find(&expired)
		for _, row := range expired {
			if ch, ok := s.waiters.LoadAndDelete(row.ID); ok {
				close(ch.(chan struct{}))
			}
		}
	}
	return result.RowsAffected, nil
}

// RecoverPending handles approvals left in "pending" state from a previous
// server instance. Approvals older than maxAge are marked as "expired";
// more recent ones get fresh waiter channels registered so they can still
// be resolved via the HTTP API.
// Writes are serialized via writeMu to avoid SQLITE_BUSY with Create/Resolve.
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

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
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
