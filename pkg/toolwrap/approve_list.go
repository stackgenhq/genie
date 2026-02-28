// Package toolwrap provides an in-memory approve list for temporary HITL bypass.
// Users can add a tool to the list when approving so future calls are auto-approved
// for a chosen duration (blind or when args contain given strings).

package toolwrap

import (
	"strings"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/ttlcache"
)

const maxApproveListBlindSize = 256

// ApproveList is an in-memory, thread-safe list of tool approvals that bypass
// the HITL gate for a limited duration. Blind entries are stored in a TTL+LRU
// cache so expired entries are evicted on access and size is bounded. Filter
// entries are pruned opportunistically when adding new entries.
type ApproveList struct {
	mu     sync.RWMutex
	blind  *ttlcache.TTLMap[struct{}] // toolName -> present; TTL+LRU eviction
	filter []approveListFilter        // toolName + args substrings + expiresAt
}

// approveListFilter is one entry for "approve when args contain any of these strings".
type approveListFilter struct {
	toolName   string
	substrings []string
	expiresAt  time.Time
}

// NewApproveList creates an empty in-memory approve list.
func NewApproveList() *ApproveList {
	return &ApproveList{
		blind:  ttlcache.NewTTLMap[struct{}](maxApproveListBlindSize, 10*time.Minute),
		filter: nil,
	}
}

// AddBlind adds a tool to the approve list for the given duration.
// Any call to that tool within the duration is auto-approved regardless of args.
func (l *ApproveList) AddBlind(toolName string, duration time.Duration) {
	if duration <= 0 {
		return
	}
	key := strings.ToLower(toolName)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.blind.SetWithTTL(key, struct{}{}, duration)
	l.pruneExpiredFilterLocked(time.Now())
}

// AddWithArgsFilter adds a tool to the approve list when args contain any of
// the given substrings, for the given duration. If substrings is empty, it
// behaves like AddBlind for that tool.
func (l *ApproveList) AddWithArgsFilter(toolName string, substrings []string, duration time.Duration) {
	if duration <= 0 {
		return
	}
	now := time.Now()
	key := strings.ToLower(toolName)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneExpiredFilterLocked(now)
	if len(substrings) == 0 {
		l.blind.SetWithTTL(key, struct{}{}, duration)
		return
	}
	subs := make([]string, len(substrings))
	copy(subs, substrings)
	l.filter = append(l.filter, approveListFilter{
		toolName:   key,
		substrings: subs,
		expiresAt:  now.Add(duration),
	})
}

// IsApproved returns true if the tool is on the approve list and not expired.
// For blind entries, any args match. For filter entries, args must contain
// at least one of the stored substrings. Blind entries use a TTL+LRU cache so
// expired entries are evicted on access.
func (l *ApproveList) IsApproved(toolName string, args string) bool {
	key := strings.ToLower(toolName)

	l.mu.RLock()
	_, blindOK := l.blind.Get(key)
	l.mu.RUnlock()
	if blindOK {
		return true
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	now := time.Now()
	argsLower := strings.ToLower(args)
	for i := range l.filter {
		f := &l.filter[i]
		if f.toolName != key || now.After(f.expiresAt) {
			continue
		}
		for _, sub := range f.substrings {
			if strings.Contains(argsLower, strings.ToLower(sub)) {
				return true
			}
		}
	}
	return false
}

// pruneExpiredFilterLocked removes expired entries from l.filter. Caller must hold l.mu (write lock).
// Zeros truncated elements so the slice can be garbage-collected and avoids retaining references.
func (l *ApproveList) pruneExpiredFilterLocked(now time.Time) {
	n := 0
	for i := range l.filter {
		if now.Before(l.filter[i].expiresAt) {
			l.filter[n] = l.filter[i]
			n++
		}
	}
	for i := n; i < len(l.filter); i++ {
		l.filter[i] = approveListFilter{}
	}
	l.filter = l.filter[:n]
}
