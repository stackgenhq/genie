/*
Copyright © 2026 StackGen, Inc.
*/

// Package clarify provides an in-memory store for clarifying questions
// that the LLM can ask the user. It follows the same channel-based
// block/wait pattern as the HITL approval flow but is lighter weight:
// no database persistence, no audit trail — just ephemeral Q&A.
package clarify

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// Request represents a pending clarifying question.
type Request struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Context  string `json:"context,omitempty"` // optional context about why the LLM is asking
}

// Response is the user's answer to a clarifying question.
type Response struct {
	Answer string `json:"answer"`
}

// Store manages pending clarification requests using in-process channels.
// Thread-safe for concurrent use.
type Store struct {
	mu      sync.Mutex
	pending map[string]chan Response
}

// NewStore creates a new clarification store.
func NewStore() *Store {
	return &Store{
		pending: make(map[string]chan Response),
	}
}

// AskWithID creates a pending clarification request and returns the ID
// the event to the UI before blocking.
func (s *Store) AskWithID(question string) (string, chan Response) {
	id := uuid.NewString()
	ch := make(chan Response, 1)

	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()

	return id, ch
}

// Cleanup removes a pending request (called when waiting is done).
func (s *Store) Cleanup(id string) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

// Respond delivers the user's answer to a pending clarification request.
// Returns an error if the request ID is not found (expired or already answered).
func (s *Store) Respond(id, answer string) error {
	s.mu.Lock()
	ch, ok := s.pending[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("clarification request %q not found or already answered", id)
	}

	// Non-blocking send — the channel has buffer size 1.
	select {
	case ch <- Response{Answer: answer}:
		return nil
	default:
		return fmt.Errorf("clarification request %q already answered", id)
	}
}
