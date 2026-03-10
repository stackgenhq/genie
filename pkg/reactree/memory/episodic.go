// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//
// Change Date: 2029-03-10
// Change License: Apache License, Version 2.0

package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
)

// EpisodeStatus records how an agent node terminated.
type EpisodeStatus string

const (
	// EpisodeSuccess means the agent completed its subgoal.
	EpisodeSuccess EpisodeStatus = "success"
	// EpisodeFailure means the agent failed its subgoal.
	EpisodeFailure EpisodeStatus = "failure"
	// EpisodeExpand means the agent decomposed into sub-tasks.
	EpisodeExpand EpisodeStatus = "expand"
	// EpisodePending means the episode is awaiting human validation.
	// Episodes are initially stored as pending until a user reacts
	// (e.g. 👍 upgrades to success, 👎 downgrades to failure).
	EpisodePending EpisodeStatus = "pending"
)

// Episode records a single subgoal-level experience for episodic memory.
// Each episode stores the goal, the full text trajectory, and the termination status.
// This granularity enables targeted in-context example retrieval per the paper.
type Episode struct {
	Goal       string        `json:"goal"`
	Trajectory string        `json:"trajectory"`
	Status     EpisodeStatus `json:"status"`
}

// episodeTopicPrefix is used to tag episodes with their status in memory.Service topics.
const episodeTopicPrefix = "reactree:episode:"

//go:generate go tool counterfeiter -generate

// EpisodicMemory stores and retrieves past subgoal-level experiences.
// Implementations can use embedding similarity (e.g., Sentence-BERT)
// to find the most relevant past experiences for a given goal.
//
//counterfeiter:generate . EpisodicMemory
type EpisodicMemory interface {
	// Store saves a completed episode into the memory.
	Store(ctx context.Context, episode Episode)

	// Retrieve finds the top-k most similar episodes for the given goal.
	Retrieve(ctx context.Context, goal string, k int) []Episode
}

// serviceEpisodicMemory delegates to trpc-agent-go's memory.Service.
// Episodes are serialized as JSON content with the goal as the search key.
// The status is stored as a topic for potential filtering.
type serviceEpisodicMemory struct {
	svc     memory.Service
	userKey memory.UserKey
}

// EpisodicMemoryConfig holds configuration for creating a memory.Service-backed
// EpisodicMemory implementation.
type EpisodicMemoryConfig struct {
	// Service is the trpc-agent-go memory.Service backend.
	Service memory.Service

	// AppName identifies the application for memory scoping.
	AppName string

	// UserID identifies the user/session for memory scoping.
	UserID string
}

// WithUserID returns a copy of the config with the UserID set to the given value.
// This enables creating per-sender episodic memory from a shared base config.
func (cfg EpisodicMemoryConfig) WithUserID(userID string) EpisodicMemoryConfig {
	cfg.UserID = userID
	return cfg
}

func DefaultEpisodicMemoryConfig() EpisodicMemoryConfig {
	return EpisodicMemoryConfig{
		AppName: "reactree",
		UserID:  "default",
		Service: inmemory.NewMemoryService(),
	}
}

// NewServiceEpisodicMemory creates an EpisodicMemory backed by memory.Service.
// Episodes are stored as JSON-serialized content and searched by goal text.
func (cfg EpisodicMemoryConfig) NewEpisodicMemory() EpisodicMemory {
	return &serviceEpisodicMemory{
		svc: cfg.Service,
		userKey: memory.UserKey{
			AppName: cfg.AppName,
			UserID:  cfg.UserID,
		},
	}
}

// Store serializes the episode as JSON and calls memory.Service.AddMemory.
// The episode status is passed as a topic for downstream filtering.
func (s *serviceEpisodicMemory) Store(ctx context.Context, episode Episode) {
	content, err := json.Marshal(episode)
	if err != nil {
		return // Best-effort; don't fail the tree run for a memory write error
	}

	topics := []string{
		episodeTopicPrefix + string(episode.Status),
	}

	_ = s.svc.AddMemory(ctx, s.userKey, string(content), topics)
}

// Retrieve searches memory.Service with the goal text and deserializes
// matching entries back into Episode structs.
func (s *serviceEpisodicMemory) Retrieve(ctx context.Context, goal string, k int) []Episode {
	entries, err := s.svc.SearchMemories(ctx, s.userKey, goal)
	if err != nil || len(entries) == 0 {
		return nil
	}

	// Limit to k results
	if len(entries) > k {
		entries = entries[:k]
	}

	episodes := make([]Episode, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.Memory == nil {
			continue
		}

		var ep Episode
		if err := json.Unmarshal([]byte(entry.Memory.Memory), &ep); err != nil {
			// Fallback: treat the raw memory content as the trajectory
			ep = Episode{
				Goal:       goal,
				Trajectory: entry.Memory.Memory,
				Status:     statusFromTopics(entry.Memory.Topics),
			}
		}
		episodes = append(episodes, ep)
	}

	return episodes
}

// statusFromTopics extracts the EpisodeStatus from memory topics.
func statusFromTopics(topics []string) EpisodeStatus {
	for _, t := range topics {
		if len(t) > len(episodeTopicPrefix) {
			status := EpisodeStatus(t[len(episodeTopicPrefix):])
			switch status {
			case EpisodeSuccess, EpisodeFailure, EpisodeExpand, EpisodePending:
				return status
			}
		}
	}
	return EpisodeSuccess // Default
}

// noOpEpisodicMemory is a true no-op implementation of EpisodicMemory.
// Store is a no-op and Retrieve always returns nil.
type noOpEpisodicMemory struct{}

func (noOpEpisodicMemory) Store(_ context.Context, _ Episode) {}

func (noOpEpisodicMemory) Retrieve(_ context.Context, _ string, _ int) []Episode { return nil }

// NewNoOpEpisodicMemory creates an episodic memory that does nothing.
// Store is a no-op and Retrieve always returns nil. This is suitable for
// initial use cases that don't yet have an experience corpus, and avoids
// the overhead of constructing a real memory.Service backend.
func NewNoOpEpisodicMemory() EpisodicMemory {
	return noOpEpisodicMemory{}
}

// String formats an episode for inclusion in an LLM prompt.
func (ep Episode) String() string {
	return fmt.Sprintf("### Goal: %s (outcome: %s)\n%s", ep.Goal, ep.Status, ep.Trajectory)
}
