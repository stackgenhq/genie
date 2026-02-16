package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Ensure MemoryStore implements memory.Service
var _ memory.Service = (*MemoryStore)(nil)

// MemoryStore implements memory.Service backed by GORM.
type MemoryStore struct {
	db *gorm.DB
}

// NewMemoryService creates a new GORM-backed memory service.
func NewMemoryService(db *gorm.DB) *MemoryStore {
	return &MemoryStore{db: db}
}

// AddMemory adds or updates a memory for a user (idempotent).
// Uniques ID is generated from (AppName, UserID, Content) hash.
func (s *MemoryStore) AddMemory(ctx context.Context, userKey memory.UserKey, content string, topics []string) error {
	if err := userKey.CheckUserKey(); err != nil {
		return err
	}

	// Generate deterministic ID for idempotency
	memoryID := uuid.NewSHA1(uuid.Nil, []byte(fmt.Sprintf("%s:%s:%s", userKey.AppName, userKey.UserID, content)))

	topicsJSON, err := json.Marshal(topics)
	if err != nil {
		return fmt.Errorf("failed to marshal topics: %w", err)
	}

	mem := Memory{
		ID:      memoryID,
		AppName: userKey.AppName,
		UserID:  userKey.UserID,
		Content: content,
		Topics:  string(topicsJSON),
	}

	// Upsert: On conflict (ID), update content/topics/updated_at
	// Note: Since ID is derived from content, content won't change, but topics might.
	result := s.db.WithContext(ctx).Save(&mem)
	if result.Error != nil {
		return fmt.Errorf("failed to save memory: %w", result.Error)
	}
	return nil
}

// UpdateMemory updates an existing memory for a user.
func (s *MemoryStore) UpdateMemory(ctx context.Context, memoryKey memory.Key, content string, topics []string) error {
	if err := memoryKey.CheckMemoryKey(); err != nil {
		return err
	}

	topicsJSON, err := json.Marshal(topics)
	if err != nil {
		return fmt.Errorf("failed to marshal topics: %w", err)
	}

	var memory Memory
	result := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ? AND id = ?", memoryKey.AppName, memoryKey.UserID, memoryKey.MemoryID).
		First(&memory)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return fmt.Errorf("memory not found: %s", memoryKey.MemoryID)
		}
		return fmt.Errorf("failed to find memory: %w", result.Error)
	}

	memory.Content = content
	memory.Topics = string(topicsJSON)
	memory.UpdatedAt = time.Now() // Manually update UpdatedAt as Save doesn't always do it for existing records

	result = s.db.WithContext(ctx).Save(&memory)

	if result.Error != nil {
		return fmt.Errorf("failed to update memory: %w", result.Error)
	}
	return nil
}

// DeleteMemory deletes a memory for a user.
func (s *MemoryStore) DeleteMemory(ctx context.Context, memoryKey memory.Key) error {
	if err := memoryKey.CheckMemoryKey(); err != nil {
		return err
	}

	result := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ? AND id = ?", memoryKey.AppName, memoryKey.UserID, memoryKey.MemoryID).
		Delete(&Memory{})

	if result.Error != nil {
		return fmt.Errorf("failed to delete memory: %w", result.Error)
	}
	// GORM Delete with WHERE and no primary key might allow multiple deletes, but ID should be unique.
	// We don't strictly check RowsAffected here as idempotent delete is fine.
	return nil
}

// ClearMemories clears all memories for a user.
func (s *MemoryStore) ClearMemories(ctx context.Context, userKey memory.UserKey) error {
	if err := userKey.CheckUserKey(); err != nil {
		return err
	}

	result := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ?", userKey.AppName, userKey.UserID).
		Delete(&Memory{})

	if result.Error != nil {
		return fmt.Errorf("failed to clear memories: %w", result.Error)
	}
	return nil
}

// ReadMemories reads memories for a user.
func (s *MemoryStore) ReadMemories(ctx context.Context, userKey memory.UserKey, limit int) ([]*memory.Entry, error) {
	if err := userKey.CheckUserKey(); err != nil {
		return nil, err
	}

	var rows []Memory
	query := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ?", userKey.AppName, userKey.UserID).
		Order("updated_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to read memories: %w", err)
	}

	return s.toEntries(rows)
}

// SearchMemories searches memories for a user using basic text matching.
func (s *MemoryStore) SearchMemories(ctx context.Context, userKey memory.UserKey, query string) ([]*memory.Entry, error) {
	if err := userKey.CheckUserKey(); err != nil {
		return nil, err
	}

	var rows []Memory
	// Basic LIKE search. For production, consider FTS or Vector DB.
	sql := "app_name = ? AND user_id = ? AND content LIKE ?"
	likeQuery := "%" + query + "%"

	if err := s.db.WithContext(ctx).
		Where(sql, userKey.AppName, userKey.UserID, likeQuery).
		Order("updated_at DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to search memories: %w", err)
	}

	return s.toEntries(rows)
}

// Tools returns supported tools. Currently none for this adapter.
func (s *MemoryStore) Tools() []tool.Tool {
	return []tool.Tool{}
}

// EnqueueAutoMemoryJob is a no-op for now.
func (s *MemoryStore) EnqueueAutoMemoryJob(ctx context.Context, sess *session.Session) error {
	return nil
}

// Close is a no-op as DB lifecycle is managed externally.
func (s *MemoryStore) Close() error {
	return nil
}

// Helper to convert GORM models to memory.Entry
func (s *MemoryStore) toEntries(rows []Memory) ([]*memory.Entry, error) {
	entries := make([]*memory.Entry, len(rows))
	for i, r := range rows {
		var topics []string
		if r.Topics != "" {
			if err := json.Unmarshal([]byte(r.Topics), &topics); err != nil {
				// Log error but continue? Or return error?
				// For now, return empty topics on error
				topics = []string{}
			}
		}

		entries[i] = &memory.Entry{
			ID:      r.ID.String(),
			AppName: r.AppName,
			UserID:  r.UserID,
			Memory: &memory.Memory{
				Memory:      r.Content,
				Topics:      topics,
				LastUpdated: &r.UpdatedAt,
			},
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
	}
	return entries, nil
}
