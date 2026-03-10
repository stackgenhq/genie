// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package memory

import "sync"

// WorkingMemory is a thread-safe key-value store shared across all agent nodes
// in a single ReAcTree run. It enables agents to share environment-specific
// observations (e.g., discovered file content, resource locations) without
// re-exploring the environment.
// Without this, each agent node would operate in isolation, potentially
// duplicating costly exploration actions.
type WorkingMemory struct {
	mu    sync.RWMutex
	store map[string]string
}

// NewWorkingMemory creates an empty WorkingMemory instance.
func NewWorkingMemory() *WorkingMemory {
	return &WorkingMemory{
		store: make(map[string]string),
	}
}

// Store saves a key-value pair into working memory.
// If the key already exists, its value is overwritten with the latest observation.
func (m *WorkingMemory) Store(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = value
}

// Recall retrieves the value for a given key from working memory.
// Returns the value and true if the key exists, or empty string and false otherwise.
func (m *WorkingMemory) Recall(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.store[key]
	return v, ok
}

// Keys returns all keys currently stored in working memory.
// This is useful for agents to discover what observations are available.
func (m *WorkingMemory) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.store))
	for k := range m.store {
		keys = append(keys, k)
	}
	return keys
}

// Clear removes all entries from working memory.
func (m *WorkingMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = make(map[string]string)
}

// Snapshot returns a shallow copy of the current memory state.
// Useful for logging or passing a read-only view to prompts.
func (m *WorkingMemory) Snapshot() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap := make(map[string]string, len(m.store))
	for k, v := range m.store {
		snap[k] = v
	}
	return snap
}
