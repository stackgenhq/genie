// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ttlcache

import (
	"container/list"
	"sync"
	"time"
)

// TTLMap is a thread-safe, bounded key-value cache with per-entry TTL and LRU
// eviction. Entries expire after DefaultTTL and are lazily cleaned up on
// access. When the map reaches MaxSize, the least-recently-used entry is
// evicted to make room.
//
// Usage:
//
//	m := ttlcache.NewTTLMap[string](128, 2*time.Minute)
//	m.Set("key", "value")
//	val, ok := m.Get("key") // ok=true if present and not expired
type TTLMap[V any] struct {
	mu         sync.Mutex
	maxSize    int
	defaultTTL time.Duration
	items      map[string]*list.Element
	evictList  *list.List
}

// ttlEntry holds a single cached value along with its expiry time.
type ttlEntry[V any] struct {
	key       string
	value     V
	expiresAt time.Time
}

// NewTTLMap creates a TTLMap that holds at most maxSize entries and assigns
// the given defaultTTL to every new entry.
func NewTTLMap[V any](maxSize int, defaultTTL time.Duration) *TTLMap[V] {
	if maxSize <= 0 {
		maxSize = 128
	}
	return &TTLMap[V]{
		maxSize:    maxSize,
		defaultTTL: defaultTTL,
		items:      make(map[string]*list.Element, maxSize),
		evictList:  list.New(),
	}
}

// Get retrieves the value for key. Returns (value, true) if the key exists and
// has not expired, or (zero, false) otherwise. Expired entries are removed
// lazily on access. A successful Get promotes the entry to the front (MRU).
func (m *TTLMap[V]) Get(key string) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	el, ok := m.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	entry := el.Value.(*ttlEntry[V])
	if time.Now().After(entry.expiresAt) {
		m.removeElement(el)
		var zero V
		return zero, false
	}
	// Promote to front (most recently used).
	m.evictList.MoveToFront(el)
	return entry.value, true
}

// Set stores a value for the given key with the default TTL. If the key
// already exists, the value and expiry are updated and the entry is promoted.
// If the map is at capacity, the least-recently-used entry is evicted.
func (m *TTLMap[V]) Set(key string, value V) {
	m.SetWithTTL(key, value, m.defaultTTL)
}

// SetWithTTL stores a value with a custom TTL.
func (m *TTLMap[V]) SetWithTTL(key string, value V, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if el, ok := m.items[key]; ok {
		// Update existing entry.
		entry := el.Value.(*ttlEntry[V])
		entry.value = value
		entry.expiresAt = time.Now().Add(ttl)
		m.evictList.MoveToFront(el)
		return
	}

	// Evict the least recently used entry if at capacity.
	if m.evictList.Len() >= m.maxSize {
		m.evictOldest()
	}

	entry := &ttlEntry[V]{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	el := m.evictList.PushFront(entry)
	m.items[key] = el
}

// Delete removes a key from the cache.
func (m *TTLMap[V]) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		m.removeElement(el)
	}
}

// Len returns the number of entries (including expired but not yet cleaned up).
func (m *TTLMap[V]) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.evictList.Len()
}

// removeElement removes element from both evictList and items map.
// Caller must hold m.mu.
func (m *TTLMap[V]) removeElement(el *list.Element) {
	m.evictList.Remove(el)
	entry := el.Value.(*ttlEntry[V])
	delete(m.items, entry.key)
}

// evictOldest removes the least-recently-used entry.
// Caller must hold m.mu.
func (m *TTLMap[V]) evictOldest() {
	el := m.evictList.Back()
	if el != nil {
		m.removeElement(el)
	}
}
