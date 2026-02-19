/*
Copyright © 2026 StackGen, Inc.
*/

package clarify

import (
	"context"
	"fmt"
	"sync"

	"github.com/appcd-dev/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// maxAskedQuestions bounds the dedup map to prevent unbounded growth
// over long-running sessions. When exceeded, oldest entries are evicted.
const maxAskedQuestions = 64

// ClarificationEvent is emitted to the UI when the tool asks a question.
// Defined here to avoid importing agui (which imports clarify → cycle).
type ClarificationEvent struct {
	RequestID string
	Question  string
	Context   string
}

// EventEmitter sends a ClarificationEvent to the UI layer.
// The implementation is provided by the agui package at wiring time.
type EventEmitter func(ctx context.Context, evt ClarificationEvent) error

// AskClarifyingQuestionRequest is the input schema for the tool.
type AskClarifyingQuestionRequest struct {
	Question string `json:"question" jsonschema:"description=The question to ask the user. Be specific and concise.,required"`
	Context  string `json:"context"  jsonschema:"description=Brief context explaining why you need this information. Helps the user give a better answer."`
}

// NewTool creates the ask_clarifying_question tool.
// The emitter is called to send clarification events to the UI.
func (clarifyStore *Store) NewTool(emitter EventEmitter) tool.CallableTool {
	t := &askClarifyTool{
		store:      clarifyStore,
		emitter:    emitter,
		asked:      make(map[string]string),
		askedOrder: make([]string, 0, maxAskedQuestions),
	}
	return function.NewFunctionTool(
		t.Do,
		function.WithName("ask_clarifying_question"),
		function.WithDescription(
			"Ask the user a clarifying question when you need more information to proceed. "+
				"The tool blocks until the user responds. Use this when the task is ambiguous, "+
				"you need to choose between alternatives, or critical details are missing. "+
				"Do NOT use this for confirmation — use regular tool calls for that.",
		),
	)
}

type askClarifyTool struct {
	store      *Store
	emitter    EventEmitter
	mu         sync.Mutex
	asked      map[string]string // question → requestID (dedup within session)
	askedOrder []string          // FIFO order for eviction
}

// Do executes the tool: emits a clarification event to the UI and blocks
// until the user responds.
func (t *askClarifyTool) Do(ctx context.Context, req AskClarifyingQuestionRequest) (string, error) {
	log := logger.GetLogger(ctx).With("fn", "clarify.ask_clarifying_question")

	if req.Question == "" {
		return "", fmt.Errorf("question is required")
	}

	// Dedup: if the exact same question was already asked in this session,
	// return immediately so the LLM doesn't emit a duplicate UI card.
	t.mu.Lock()
	if prevID, exists := t.asked[req.Question]; exists {
		t.mu.Unlock()
		log.Info("duplicate clarification suppressed", "question", req.Question, "original_id", prevID)
		return "This question was already asked. Please wait for the user's response to the original question.", nil
	}

	// Create pending request and get the ID + response channel.
	reqID, ch := t.store.AskWithID(req.Question)
	t.asked[req.Question] = reqID
	t.askedOrder = append(t.askedOrder, req.Question)

	// Evict oldest entries if the map exceeds capacity.
	for len(t.asked) > maxAskedQuestions {
		oldest := t.askedOrder[0]
		t.askedOrder = t.askedOrder[1:]
		delete(t.asked, oldest)
	}
	t.mu.Unlock()

	defer t.store.Cleanup(reqID)

	log.Info("asking clarifying question", "id", reqID, "question", req.Question)

	// Emit the clarification request event to the UI.
	if t.emitter != nil {
		if err := t.emitter(ctx, ClarificationEvent{
			RequestID: reqID,
			Question:  req.Question,
			Context:   req.Context,
		}); err != nil {
			return "", fmt.Errorf("failed to emit clarification event: %w", err)
		}
	}

	// Block until the user responds or context times out.
	select {
	case resp := <-ch:
		log.Info("received clarification answer", "id", reqID, "answer_len", len(resp.Answer))
		return fmt.Sprintf("User's answer: %s", resp.Answer), nil
	case <-ctx.Done():
		return "", fmt.Errorf("no response received for question %q — the user did not answer in time", req.Question)
	}
}
