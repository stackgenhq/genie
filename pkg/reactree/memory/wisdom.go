// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package memory

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
	memoryService "trpc.group/trpc-go/trpc-agent-go/memory"
)

// WisdomNote is a concise, distilled summary of raw episodes from a time
// period (e.g., one day). Instead of injecting 20 raw episodes into a prompt,
// the consolidator batches them into 1-2 wisdom notes that capture the
// essential lessons.
//
// Format:
//
//	"On <date>, you learned:
//	 - <lesson 1>
//	 - <lesson 2>
//	 ..."
type WisdomNote struct {
	// Summary is the consolidated text from multiple episodes.
	Summary string `json:"summary"`

	// Period is the time range this note covers (e.g., "2026-03-10").
	Period string `json:"period"`

	// EpisodeCount is how many raw episodes were distilled into this note.
	EpisodeCount int `json:"episode_count"`

	// CreatedAt is when this wisdom note was generated.
	CreatedAt time.Time `json:"created_at"`
}

const wisdomMemoryTopic = "reactree:wisdom"

// WisdomStore stores and retrieves consolidated wisdom notes.
// It uses the same memory.Service backend as episodic memory.
//
//counterfeiter:generate . WisdomStore
type WisdomStore interface {
	// StoreWisdom saves a consolidated wisdom note.
	StoreWisdom(ctx context.Context, note WisdomNote)

	// RetrieveWisdom returns the most recent wisdom notes (up to limit).
	RetrieveWisdom(ctx context.Context, limit int) []WisdomNote
}

// serviceWisdomStore implements WisdomStore using memory.Service.
type serviceWisdomStore struct {
	svc     memoryService.Service
	userKey memoryService.UserKey
}

// WisdomStoreConfig holds configuration for creating a WisdomStore.
type WisdomStoreConfig struct {
	Service memoryService.Service
	AppName string
	UserID  string
}

// NewWisdomStore creates a WisdomStore backed by memory.Service.
func (cfg WisdomStoreConfig) NewWisdomStore() WisdomStore {
	return &serviceWisdomStore{
		svc: cfg.Service,
		userKey: memoryService.UserKey{
			AppName: cfg.AppName,
			UserID:  cfg.UserID,
		},
	}
}

// StoreWisdom saves a wisdom note to memory.Service.
func (s *serviceWisdomStore) StoreWisdom(ctx context.Context, note WisdomNote) {
	logr := logger.GetLogger(ctx).With("fn", "WisdomStore.StoreWisdom")

	if note.CreatedAt.IsZero() {
		note.CreatedAt = time.Now()
	}

	content, err := json.Marshal(note)
	if err != nil {
		logr.Warn("failed to marshal wisdom note", "error", err)
		return
	}

	if err := s.svc.AddMemory(ctx, s.userKey, string(content), []string{wisdomMemoryTopic, note.Period}); err != nil {
		logr.Warn("failed to store wisdom note", "error", err)
	}
}

// RetrieveWisdom reads the most recent wisdom notes.
func (s *serviceWisdomStore) RetrieveWisdom(ctx context.Context, limit int) []WisdomNote {
	if limit <= 0 {
		return nil
	}

	logr := logger.GetLogger(ctx).With("fn", "WisdomStore.RetrieveWisdom")

	entries, err := s.svc.ReadMemories(ctx, s.userKey, limit*3) // Over-read to filter by topic
	if err != nil {
		logr.Warn("failed to read wisdom notes", "error", err)
		return nil
	}

	var notes []WisdomNote
	for _, entry := range entries {
		// Filter by wisdom topic.
		isWisdom := false
		for _, t := range entry.Memory.Topics {
			if t == wisdomMemoryTopic {
				isWisdom = true
				break
			}
		}
		if !isWisdom {
			continue
		}

		var note WisdomNote
		if err := json.Unmarshal([]byte(entry.Memory.Memory), &note); err != nil {
			continue
		}
		notes = append(notes, note)
		if len(notes) >= limit {
			break
		}
	}

	return notes
}

// noOpWisdomStore is a no-op implementation for when storage is not configured.
type noOpWisdomStore struct{}

func (noOpWisdomStore) StoreWisdom(_ context.Context, _ WisdomNote) {}
func (noOpWisdomStore) RetrieveWisdom(_ context.Context, _ int) []WisdomNote {
	return nil
}

// NewNoOpWisdomStore creates a WisdomStore that discards all writes and
// returns no results.
func NewNoOpWisdomStore() WisdomStore {
	return noOpWisdomStore{}
}
