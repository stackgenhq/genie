package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/appcd-dev/genie/pkg/db"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:generate go tool counterfeiter -generate

// ListTasksRequest specifies filters for listing cron tasks.
type ListTasksRequest struct {
	EnabledOnly bool
}

// CreateTaskRequest holds the parameters for creating a new cron task.
type CreateTaskRequest struct {
	Name       string
	Expression string
	Action     string
	Source     string // "config" or "tool"
}

// DeleteTaskRequest identifies the cron task to delete.
type DeleteTaskRequest struct {
	ID uuid.UUID
}

// RecordRunRequest holds the parameters for recording a cron execution.
type RecordRunRequest struct {
	TaskID   uuid.UUID
	TaskName string
	Status   db.CronStatus // "running", "success", "failed"
	Error    string
	RunID    string
}

// UpdateRunRequest holds the parameters for updating a completed cron run.
type UpdateRunRequest struct {
	HistoryID uuid.UUID
	Status    db.CronStatus
	Error     string
	RunID     string
}

// RecentFailuresRequest specifies how many recent failures to retrieve.
type RecentFailuresRequest struct {
	Limit int
}

// ICronStore defines the repository interface for cron task persistence.
// Implementations must be safe for concurrent use.
//
//counterfeiter:generate . ICronStore
type ICronStore interface {
	// ListTasks returns all cron tasks, optionally filtered to enabled-only.
	ListTasks(ctx context.Context, req ListTasksRequest) ([]CronTask, error)
	// CreateTask persists a new cron task, upserting on name conflict.
	CreateTask(ctx context.Context, req CreateTaskRequest) (*CronTask, error)
	// DeleteTask removes a cron task by ID.
	DeleteTask(ctx context.Context, req DeleteTaskRequest) error
	// DueTasks returns all enabled tasks whose NextRunAt <= now.
	DueTasks(ctx context.Context, now time.Time) ([]CronTask, error)
	// MarkTriggered sets LastTriggeredAt for a task.
	MarkTriggered(ctx context.Context, taskID uuid.UUID, triggeredAt time.Time) error
	// SetNextRun updates the pre-computed NextRunAt for a task.
	SetNextRun(ctx context.Context, taskID uuid.UUID, nextRun time.Time) error
	// RecordRun creates a new cron_history entry for an execution.
	RecordRun(ctx context.Context, req RecordRunRequest) (*CronHistory, error)
	// UpdateRun updates a cron_history entry when execution completes.
	UpdateRun(ctx context.Context, req UpdateRunRequest) error
	// RecentFailures returns the most recent failed cron runs.
	RecentFailures(ctx context.Context, req RecentFailuresRequest) ([]CronHistory, error)
	// CleanupHistory deletes cron_history entries older than the given duration.
	CleanupHistory(ctx context.Context, olderThan time.Duration) (int64, error)
}

// Store implements ICronStore backed by GORM.
// It provides CRUD operations for cron tasks and audit history.
type Store struct {
	db *gorm.DB
}

// NewStore creates a new GORM-backed cron store.
// The caller is responsible for running AutoMigrate before using the store.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// ListTasks returns all cron tasks, optionally filtered to enabled-only.
// Without this method, the scheduler would have no way to discover which
// tasks to register on startup.
func (s *Store) ListTasks(ctx context.Context, req ListTasksRequest) ([]CronTask, error) {
	var tasks []CronTask
	query := s.db.WithContext(ctx)
	if req.EnabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Order("created_at ASC").Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("failed to list cron tasks: %w", err)
	}
	return tasks, nil
}

// CreateTask persists a new cron task.
// If a task with the same name already exists, it updates the expression,
// action, and source fields (upsert semantics).
func (s *Store) CreateTask(ctx context.Context, req CreateTaskRequest) (*CronTask, error) {
	task := CronTask{
		Name:       req.Name,
		Expression: req.Expression,
		Action:     req.Action,
		Enabled:    true,
		Source:     req.Source,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.
			Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "name"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"expression", "action", "source", "enabled", "updated_at",
				}),
			}).
			Create(&task)
		if result.Error != nil {
			return fmt.Errorf("failed to upsert cron task: %w", result.Error)
		}
		// Re-fetch the task to ensure we return the persisted ID and other DB-managed fields.
		if err := tx.Where("name = ?", req.Name).First(&task).Error; err != nil {
			return fmt.Errorf("failed to load cron task after upsert: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// DeleteTask removes a cron task by ID.
// Without this method, tasks could not be cleaned up or unscheduled.
func (s *Store) DeleteTask(ctx context.Context, req DeleteTaskRequest) error {
	result := s.db.WithContext(ctx).Where("id = ?", req.ID).Delete(&CronTask{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete cron task: %w", result.Error)
	}
	return nil
}

// RecordRun creates a new cron_history entry marking the start of an execution.
// Returns the history entry so callers can later update it via UpdateRun.
// Without this method, there would be no audit trail for cron executions.
func (s *Store) RecordRun(ctx context.Context, req RecordRunRequest) (*CronHistory, error) {
	history := CronHistory{
		ID:        uuid.New(),
		TaskID:    req.TaskID,
		TaskName:  req.TaskName,
		StartedAt: time.Now(),
		Status:    req.Status,
		Error:     req.Error,
		RunID:     req.RunID,
	}
	if err := s.db.WithContext(ctx).Create(&history).Error; err != nil {
		return nil, fmt.Errorf("failed to record cron run: %w", err)
	}
	return &history, nil
}

// UpdateRun updates a cron_history entry when execution completes,
// setting the finished_at timestamp, final status, and any error message.
// Without this method, runs would remain in "running" state forever.
func (s *Store) UpdateRun(ctx context.Context, req UpdateRunRequest) error {
	now := time.Now()
	updates := map[string]interface{}{
		"finished_at": now,
		"status":      req.Status,
		"error":       req.Error,
	}
	if req.RunID != "" {
		updates["run_id"] = req.RunID
	}
	result := s.db.WithContext(ctx).
		Model(&CronHistory{}).
		Where("id = ?", req.HistoryID).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("failed to update cron run: %w", result.Error)
	}
	return nil
}

// DueTasks returns all enabled tasks whose NextRunAt is at or before `now`.
// This is the core query for the DB-driven cron ticker.
func (s *Store) DueTasks(ctx context.Context, now time.Time) ([]CronTask, error) {
	var tasks []CronTask
	if err := s.db.WithContext(ctx).
		Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("next_run_at ASC").
		Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("failed to query due cron tasks: %w", err)
	}
	return tasks, nil
}

// MarkTriggered sets the LastTriggeredAt timestamp for a task.
func (s *Store) MarkTriggered(ctx context.Context, taskID uuid.UUID, triggeredAt time.Time) error {
	result := s.db.WithContext(ctx).
		Model(&CronTask{}).
		Where("id = ?", taskID).
		Update("last_triggered_at", triggeredAt)
	if result.Error != nil {
		return fmt.Errorf("failed to mark cron task triggered: %w", result.Error)
	}
	return nil
}

// SetNextRun updates the pre-computed NextRunAt for a task.
func (s *Store) SetNextRun(ctx context.Context, taskID uuid.UUID, nextRun time.Time) error {
	result := s.db.WithContext(ctx).
		Model(&CronTask{}).
		Where("id = ?", taskID).
		Update("next_run_at", nextRun)
	if result.Error != nil {
		return fmt.Errorf("failed to set next run for cron task: %w", result.Error)
	}
	return nil
}

// RecentFailures returns the most recent failed cron runs, ordered by
// started_at descending. Used by the heartbeat health check to surface
// cron problems. Without this method, the heartbeat would have no
// visibility into cron reliability.
func (s *Store) RecentFailures(ctx context.Context, req RecentFailuresRequest) ([]CronHistory, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	var failures []CronHistory
	if err := s.db.WithContext(ctx).
		Where("status = ?", db.CronStatusFailed).
		Order("started_at DESC").
		Limit(limit).
		Find(&failures).Error; err != nil {
		return nil, fmt.Errorf("failed to query recent cron failures: %w", err)
	}
	return failures, nil
}

// CleanupHistory deletes cron_history entries older than the given duration.
// Should be called periodically to prevent unbounded table growth.
func (s *Store) CleanupHistory(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result := s.db.WithContext(ctx).
		Where("started_at < ?", cutoff).
		Delete(&CronHistory{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to cleanup cron history: %w", result.Error)
	}
	return result.RowsAffected, nil
}
