package db

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ShortMemoryStore provides a generic, TTL-bounded key-value store backed by
// the short_memories table. Different subsystems share the same table but are
// logically isolated via MemoryType (e.g. "reaction_ledger", "cooldown").
//
// This replaces ad-hoc in-memory sync.Map / TTL map patterns with a durable,
// restart-safe store. Expired entries are cleaned up lazily on read and
// periodically via Cleanup().
type ShortMemoryStore struct {
	db *gorm.DB
}

// NewShortMemoryStore creates a new store backed by the given GORM DB.
func NewShortMemoryStore(db *gorm.DB) *ShortMemoryStore {
	return &ShortMemoryStore{db: db}
}

// Set inserts or updates a short memory entry with the given TTL.
// The value should be a JSON-encoded string containing subsystem-specific data.
func (s *ShortMemoryStore) Set(ctx context.Context, memoryType, key, value string, ttl time.Duration) error {
	now := time.Now()
	entry := ShortMemory{
		Key:        key,
		MemoryType: memoryType,
		Value:      value,
		ExpiresAt:  now.Add(ttl),
		CreatedAt:  now,
	}

	// Upsert: on conflict (key, memory_type), update value and expires_at.
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}, {Name: "memory_type"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "expires_at"}),
		}).
		Create(&entry)

	if result.Error != nil {
		return fmt.Errorf("short_memory set failed: %w", result.Error)
	}
	return nil
}

// Get retrieves a non-expired entry. Returns the value and true if found,
// or empty string and false if missing or expired.
func (s *ShortMemoryStore) Get(ctx context.Context, memoryType, key string) (string, bool, error) {
	var entry ShortMemory
	result := s.db.WithContext(ctx).
		Where("key = ? AND memory_type = ? AND expires_at > ?", key, memoryType, time.Now()).
		First(&entry)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, fmt.Errorf("short_memory get failed: %w", result.Error)
	}
	return entry.Value, true, nil
}

// Delete removes a specific entry.
func (s *ShortMemoryStore) Delete(ctx context.Context, memoryType, key string) error {
	result := s.db.WithContext(ctx).
		Where("key = ? AND memory_type = ?", key, memoryType).
		Delete(&ShortMemory{})
	if result.Error != nil {
		return fmt.Errorf("short_memory delete failed: %w", result.Error)
	}
	return nil
}

// Cleanup removes all expired entries across all memory types.
// Call this periodically (e.g. on a timer) to prevent table bloat.
func (s *ShortMemoryStore) Cleanup(ctx context.Context) (int64, error) {
	result := s.db.WithContext(ctx).
		Where("expires_at <= ?", time.Now()).
		Delete(&ShortMemory{})
	if result.Error != nil {
		return 0, fmt.Errorf("short_memory cleanup failed: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// Count returns the number of non-expired entries for a given memory type.
// Primarily useful for monitoring and testing.
func (s *ShortMemoryStore) Count(ctx context.Context, memoryType string) (int64, error) {
	var count int64
	result := s.db.WithContext(ctx).
		Model(&ShortMemory{}).
		Where("memory_type = ? AND expires_at > ?", memoryType, time.Now()).
		Count(&count)
	if result.Error != nil {
		return 0, fmt.Errorf("short_memory count failed: %w", result.Error)
	}
	return count, nil
}
