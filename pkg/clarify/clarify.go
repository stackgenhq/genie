/*
Copyright © 2026 StackGen, Inc.
*/

// Package clarify provides a durable store for clarifying questions that
// the LLM can ask the user. It uses a DB + in-process channel hybrid
// (the same pattern as the HITL approval store):
//
//   - DB layer provides durable persistence so pending questions survive
//     restarts.
//   - In-process channels provide instant notification when a user answers.
//   - A polling fallback ensures answers are detected even if the channel
//     signal is missed.
//   - RecoverPending re-registers channels on startup for questions that
//     were pending when the server last stopped.
package clarify

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/stackgenhq/genie/pkg/db"
	"github.com/stackgenhq/genie/pkg/retrier"
	"gorm.io/gorm"
)

// Request represents a pending clarifying question.
// It is typically serialized as JSON when sent over the AG-UI SSE stream.
type Request struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Context  string `json:"context,omitempty"` // optional LLM-provided context about why the question is needed
}

// Response is the user's answer to a clarifying question.
// An empty Answer is valid and indicates the user explicitly provided
// an empty response.
type Response struct {
	Answer string `json:"answer"`
}

// RecoverResult holds the outcome of [Store.RecoverPending].
type RecoverResult struct {
	Expired   int // questions older than maxAge, marked "expired" in the DB
	Recovered int // questions within maxAge, waiter channels re-registered for live use
}

// Store is the interface for managing clarification requests.
//
// A Store persists questions durably (across restarts) and provides
// a synchronous wait/notify mechanism so the calling tool goroutine
// blocks until the user answers or the context is cancelled.
//
// Typical lifecycle for a single question:
//
//	id, ch, err := store.Ask(ctx, question, qContext, senderCtx)
//	// … emit event to UI …
//	resp, err := store.WaitForResponse(ctx, id, ch)
//	store.Cleanup(id)
//
// On the answering side (HTTP handler, messenger, etc.):
//
//	err := store.Respond(requestID, userAnswer)
//
// All methods must be safe for concurrent use from multiple goroutines.
type Store interface {
	// Ask creates a pending clarification request, persists it, and returns
	// the unique request ID together with a channel that will receive the
	// user's [Response] when [Respond] is called.
	//
	// Parameters:
	//   - question: the question text displayed to the user.
	//   - qContext: optional LLM-provided context explaining why the
	//     information is needed (may be empty).
	//   - senderContext: opaque platform-specific sender identifier
	//     (e.g. "slack:U1234:C5678") used for messenger routing and
	//     persisted so [RecoverPending] can replay questions.
	//
	// The returned channel is buffered (cap 1) so [Respond] never blocks.
	// The caller must eventually call [Cleanup] to release the channel.
	Ask(ctx context.Context, question, qContext, senderContext string) (string, <-chan Response, error)

	// Respond delivers the user's answer to the pending clarification
	// identified by id. It updates the persistent store and signals any
	// goroutine blocked in [WaitForResponse].
	//
	// Returns an error if id does not exist or was already answered.
	Respond(id, answer string) error

	// WaitForResponse blocks until the clarification identified by id is
	// answered or ctx is cancelled/expired. ch must be the channel returned
	// by [Ask] for the same id.
	//
	// The implementation uses a hybrid strategy: it listens on ch for
	// immediate notification from [Respond], and polls the database every
	// 5 seconds as a fallback (in case the channel signal was missed due
	// to a race with [Cleanup] or a server restart).
	WaitForResponse(ctx context.Context, id string, ch <-chan Response) (Response, error)

	// Cleanup removes the in-process waiter channel for the given request.
	// It should be called (typically via defer) after [WaitForResponse]
	// returns, whether the question was answered or timed out.
	Cleanup(id string)

	// FindPendingByQuestion returns the ID of an existing pending
	// clarification whose question text matches the given string.
	// Used for deduplication — both within a session and across sessions.
	// Returns ("", false) if no pending match is found.
	FindPendingByQuestion(ctx context.Context, question string) (string, bool)

	// RecoverPending handles clarifications left in "pending" state from
	// a previous server instance. Questions older than maxAge are marked
	// "expired"; recent ones get fresh waiter channels so they can still
	// be answered via the API.
	//
	// This should be called once at application startup, before the
	// AG-UI server begins accepting requests.
	RecoverPending(ctx context.Context, maxAge time.Duration) (RecoverResult, error)

	// Close releases any resources held by the store (e.g. in-memory
	// waiter channels). It does not close the underlying database.
	Close()
}

// DBStore is the default [Store] implementation. It persists clarification
// requests to a GORM-backed SQLite database and uses in-process buffered
// channels for instant notification when a user answers.
//
// The design mirrors [hitl.ApprovalStore]: the DB provides durability
// across restarts while channels avoid the latency of polling.
//
// All exported methods are safe for concurrent use.
type DBStore struct {
	db      *gorm.DB
	waiters sync.Map // map[string]chan Response — keyed by request ID
}

// NewStore creates a [Store] backed by the given GORM database.
// The caller must ensure the database has been opened and migrated
// (see [db.Open] and [db.AutoMigrate]) before calling NewStore.
func NewStore(gormDB *gorm.DB) Store {
	return &DBStore{
		db: gormDB,
	}
}

// Ask persists a new "pending" clarification row and returns:
//   - id: a UUID string uniquely identifying this question.
//   - ch: a buffered channel (cap 1) that will receive a [Response]
//     when [Respond] is called for this id.
//
// The DB write is retried up to 5 times with 200 ms backoff to handle
// transient SQLITE_BUSY errors.
func (s *DBStore) Ask(ctx context.Context, question, qContext, senderContext string) (string, <-chan Response, error) {
	id := uuid.New()
	now := time.Now().UTC()

	row := db.Clarification{
		ID:            id,
		Question:      question,
		Context:       qContext,
		Status:        db.ClarifyStatusPending,
		SenderContext: senderContext,
		CreatedAt:     now,
	}

	if err := retrier.Retry(ctx, func() error {
		return s.db.WithContext(ctx).Create(&row).Error
	}, retrier.WithAttempts(5), retrier.WithBackoffDuration(200*time.Millisecond)); err != nil {
		return "", nil, fmt.Errorf("failed to persist clarification: %w", err)
	}

	// Create the waiter channel (buffered so Respond never blocks).
	ch := make(chan Response, 1)
	s.waiters.Store(id.String(), ch)

	return id.String(), ch, nil
}

// Respond atomically marks the clarification as "answered" in the DB
// and pushes the answer to the in-process waiter channel (if one exists).
//
// The DB update is retried with exponential back-off (default 3 attempts,
// 1 s interval) to handle SQLITE_BUSY under concurrent writes.
//
// Returns an error if:
//   - the request ID does not exist in the DB.
//   - the request was already answered (status ≠ "pending").
func (s *DBStore) Respond(id, answer string) error {
	now := time.Now().UTC()

	// Retry for SQLITE_BUSY under concurrent writes.
	var rowsAffected int64
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := retrier.Retry(ctx, func() error {
		result := s.db.
			Model(&db.Clarification{}).
			Where("id = ? AND status = ?", id, string(db.ClarifyStatusPending)).
			Updates(map[string]interface{}{
				"answer":      answer,
				"status":      string(db.ClarifyStatusAnswered),
				"answered_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		rowsAffected = result.RowsAffected
		return nil
	}); err != nil {
		return fmt.Errorf("failed to update clarification: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("clarification request %q not found or already answered", id)
	}

	// Signal the waiting goroutine.
	if chRaw, ok := s.waiters.LoadAndDelete(id); ok {
		ch := chRaw.(chan Response)
		select {
		case ch <- Response{Answer: answer}:
		default:
		}
	}

	return nil
}

// WaitForResponse blocks the calling goroutine until the clarification
// identified by id is answered or ctx expires.
//
// Strategy (hybrid wait/notify):
//  1. Fast-path — immediately checks the DB; returns if already answered.
//  2. Channel — selects on ch for instant notification from [Respond].
//  3. Polling — every 5 s re-queries the DB as a fallback in case the
//     channel signal was lost (e.g. [Respond] ran before the select, or
//     the answer was written by a different process after a restart).
//
// The ch parameter must be the channel returned by [Ask] for the same id.
func (s *DBStore) WaitForResponse(ctx context.Context, id string, ch <-chan Response) (Response, error) {
	// Fast path: check if already answered.
	var row db.Clarification
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err == nil {
		if row.Status == db.ClarifyStatusAnswered {
			return Response{Answer: row.Answer}, nil
		}
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case resp := <-ch:
			return resp, nil
		case <-ticker.C:
			// Polling fallback — detect answer even without channel signal.
			var pollRow db.Clarification
			if err := s.db.WithContext(ctx).Where("id = ?", id).First(&pollRow).Error; err == nil {
				if pollRow.Status == db.ClarifyStatusAnswered {
					return Response{Answer: pollRow.Answer}, nil
				}
			}
		case <-ctx.Done():
			return Response{}, fmt.Errorf("no response received — the user did not answer in time")
		}
	}
}

// Cleanup removes the in-process waiter channel for the given request ID.
// This prevents the sync.Map from growing unboundedly over long-running
// sessions. It does not modify the database row.
//
// Callers should defer Cleanup(id) immediately after [Ask] returns.
func (s *DBStore) Cleanup(id string) {
	s.waiters.Delete(id)
}

// RecoverPending scans the DB for rows with status "pending" and:
//   - Expires rows whose CreatedAt is older than maxAge (sets status
//     to "expired" and records AnsweredAt).
//   - Re-registers a fresh waiter channel for recent rows so that a
//     subsequent [Respond] call can still signal the in-process select.
//
// Call this once at application startup, before the AG-UI server
// starts accepting traffic. The [RecoverResult] tells you how many
// questions were expired vs recovered.
func (s *DBStore) RecoverPending(ctx context.Context, maxAge time.Duration) (RecoverResult, error) {
	var pending []db.Clarification
	if err := s.db.WithContext(ctx).
		Where("status = ?", string(db.ClarifyStatusPending)).
		Find(&pending).Error; err != nil {
		return RecoverResult{}, fmt.Errorf("failed to query pending clarifications: %w", err)
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
				Model(&db.Clarification{}).
				Where("id = ? AND status = ?", row.ID, string(db.ClarifyStatusPending)).
				Updates(map[string]interface{}{
					"status":      string(db.ClarifyStatusExpired),
					"answered_at": now,
				})
			result.Expired++
		} else {
			// Recent — re-register waiter channel.
			s.waiters.LoadOrStore(row.ID.String(), make(chan Response, 1))
			result.Recovered++
		}
	}

	return result, nil
}

// Close removes all in-process waiter channels from the sync.Map.
// Call this during graceful shutdown to release resources.
// It does not close the underlying database connection.
func (s *DBStore) Close() {
	s.waiters.Range(func(key, value any) bool {
		s.waiters.Delete(key)
		return true
	})
}

// FindPendingByQuestion queries the DB for a row with status "pending" and
// the exact question text. Returns (requestID, true) if found, ("", false)
// otherwise. Used by [NewTool] for cross-session deduplication.
func (s *DBStore) FindPendingByQuestion(ctx context.Context, question string) (string, bool) {
	var row db.Clarification
	err := s.db.WithContext(ctx).
		Where("question = ? AND status = ?", question, string(db.ClarifyStatusPending)).
		First(&row).Error
	if err != nil {
		return "", false
	}
	return row.ID.String(), true
}
