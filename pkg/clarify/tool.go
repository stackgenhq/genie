/*
Copyright © 2026 StackGen, Inc.
*/

package clarify

import (
	"context"
	"fmt"
	"sync"

	"github.com/appcd-dev/genie/pkg/interrupt"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// maxAskedQuestions bounds the dedup map to prevent unbounded growth
// over long-running sessions. When exceeded, oldest entries are evicted.
const maxAskedQuestions = 64

// ClarificationEvent is emitted to the UI when the LLM invokes the
// ask_clarifying_question tool. It is defined in this package (rather
// than agui) to avoid a circular import: agui imports clarify.
type ClarificationEvent struct {
	RequestID string // unique ID for this clarification, matches the DB row
	Question  string // the question text displayed to the user
	Context   string // optional LLM-provided context (may be empty)
}

// EventEmitter sends a [ClarificationEvent] to the UI layer (AG-UI SSE
// stream, messenger, etc.). The implementation is wired at application
// startup and typically lives in the app package as a closure over
// the AG-UI event channel and the messenger client.
//
// Returning a non-nil error aborts the tool call.
type EventEmitter func(ctx context.Context, evt ClarificationEvent) error

// AskClarifyingQuestionRequest is the JSON input schema for the
// ask_clarifying_question tool.
type AskClarifyingQuestionRequest struct {
	// Question is the question text shown to the user. Required.
	Question string `json:"question" jsonschema:"description=The question to ask the user. Be specific and concise.,required"`
	// Context is an optional explanation of why the LLM needs this
	// information, helping the user craft a better answer.
	Context string `json:"context"  jsonschema:"description=Brief context explaining why you need this information. Helps the user give a better answer."`
}

// ToolOption configures the behaviour of the clarification tool returned
// by [NewTool].
type ToolOption func(*askClarifyTool)

// WithNonBlocking configures the tool to return an
// [interrupt.Error] instead of blocking in [Store.WaitForResponse].
//
// Use this when the tool runs inside an executor that models human
// interaction as an interrupt/resume cycle (e.g. Temporal workflows).
// The returned InterruptError carries the request ID and a
// [ClarificationEvent] payload that the executor can use to register
// a signal wait.
//
// Default: blocking (the current in-process behaviour).
func WithNonBlocking() ToolOption {
	return func(t *askClarifyTool) { t.blocking = false }
}

// NewTool creates the "ask_clarifying_question" tool.
//
// The tool blocks the LLM goroutine until the user responds (via
// [Store.Respond]) or the context expires. The emitter is called to
// forward the question to the AG-UI SSE stream and/or messenger.
//
// Duplicate questions within the same tool instance are suppressed:
//   - In-memory dedup map (bounded to [maxAskedQuestions] entries).
//   - Cross-session dedup via [Store.FindPendingByQuestion].
//
// Pass [WithNonBlocking] to return an [interrupt.Error] instead
// of blocking; this is the forward-looking path for Temporal integration.
func NewTool(clarifyStore Store, emitter EventEmitter, opts ...ToolOption) tool.CallableTool {
	t := &askClarifyTool{
		store:      clarifyStore,
		emitter:    emitter,
		blocking:   true,
		asked:      make(map[string]string),
		askedOrder: make([]string, 0, maxAskedQuestions),
	}
	for _, o := range opts {
		o(t)
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
	store      Store
	emitter    EventEmitter
	blocking   bool // true = block in WaitForResponse; false = return InterruptError
	mu         sync.Mutex
	asked      map[string]string // question → requestID (dedup within session)
	askedOrder []string          // FIFO order for eviction
}

// Do executes the tool: emits a clarification event to the UI and blocks
// until the user responds (blocking mode) or returns an [interrupt.Error]
// (non-blocking mode).
//
// If the context carries a resume value (set by the executor via
// [interrupt.WithResumeValue] after an interrupt was resolved), Do skips
// the ask/emit/wait phases and returns the answer directly.
func (t *askClarifyTool) Do(ctx context.Context, req AskClarifyingQuestionRequest) (string, error) {
	log := logger.GetLogger(ctx).With("fn", "clarify.ask_clarifying_question")

	// Resume path: the executor is re-invoking this tool after the
	// interrupt was resolved. Return the answer without re-asking.
	if val, ok := interrupt.ResumeValueFrom(ctx); ok {
		if answer, isStr := val.(string); isStr {
			log.Info("resumed with answer", "answer_len", len(answer))
			return fmt.Sprintf("User's answer: %s", answer), nil
		}
	}

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

	// Also check the DB for pending questions with the same text (cross-session dedup).
	if prevID, found := t.store.FindPendingByQuestion(ctx, req.Question); found {
		t.asked[req.Question] = prevID
		t.mu.Unlock()
		log.Info("duplicate clarification suppressed (DB)", "question", req.Question, "original_id", prevID)
		return "This question was already asked. Please wait for the user's response to the original question.", nil
	}

	// Extract sender context for DB persistence.
	senderContext := messenger.SenderContextFrom(ctx)

	// Create pending request and persist to DB.
	reqID, ch, err := t.store.Ask(ctx, req.Question, req.Context, senderContext)
	if err != nil {
		t.mu.Unlock()
		return "", fmt.Errorf("failed to create clarification request: %w", err)
	}

	t.asked[req.Question] = reqID
	t.askedOrder = append(t.askedOrder, req.Question)

	// Evict oldest entries if the map exceeds capacity.
	for len(t.asked) > maxAskedQuestions {
		oldest := t.askedOrder[0]
		t.askedOrder = t.askedOrder[1:]
		delete(t.asked, oldest)
	}
	t.mu.Unlock()

	// In non-blocking mode, the Temporal workflow owns the channel
	// lifecycle, so we must NOT defer Cleanup here — it would delete
	// the waiter channel before anyone can send to it.
	if t.blocking {
		defer t.store.Cleanup(reqID)
	}

	log.Info("asking clarifying question", "id", reqID, "question", req.Question)

	// Emit the clarification request event to the UI.
	evt := ClarificationEvent{
		RequestID: reqID,
		Question:  req.Question,
		Context:   req.Context,
	}
	if t.emitter != nil {
		if err := t.emitter(ctx, evt); err != nil {
			return "", fmt.Errorf("failed to emit clarification event: %w", err)
		}
	}

	// Non-blocking mode: return an InterruptError so the executor can
	// model the wait as a Temporal signal or graph interrupt.
	if !t.blocking {
		return "", &interrupt.Error{
			Kind:      interrupt.Clarify,
			RequestID: reqID,
			Payload:   evt,
		}
	}

	// Blocking mode (default): wait for the user to respond.
	// Uses hybrid wait: channel for instant notification, DB polling as fallback.
	resp, err := t.store.WaitForResponse(ctx, reqID, ch)
	if err != nil {
		return "", fmt.Errorf("no response received for question %q — the user did not answer in time", req.Question)
	}

	log.Info("received clarification answer", "id", reqID, "answer_len", len(resp.Answer))
	return fmt.Sprintf("User's answer: %s", resp.Answer), nil
}
