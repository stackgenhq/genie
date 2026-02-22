package agui

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// Compile-time check that genieRunner implements runner.Runner.
var _ runner.Runner = (*genieRunner)(nil)

// ChatFunc is the function signature for the chat handler pipeline.
// It takes a context, message text, and a channel to emit events on.
// This matches the shape returned by Application.buildChatHandler().
type ChatFunc func(ctx context.Context, message string, eventChan chan<- interface{}) error

// NewRunner creates a runner.Runner that wraps the Genie chat pipeline.
//
// This adapter bridges the framework's structured runner interface to
// Genie's existing codeOwner.Chat() pipeline. The runner:
//   - Accepts a model.Message with user text
//   - Calls the chatFunc which emits *event.Event on an internal channel
//   - Forwards events to the returned channel
//
// The result is a framework-compatible runner that unlocks features like
// run registration, cancellation, and session-scoped execution while
// reusing the existing chat pipeline.
func NewRunner(chatFunc ChatFunc) runner.Runner {
	return &genieRunner{chatFunc: chatFunc}
}

// genieRunner implements runner.Runner by wrapping a Genie ChatFunc.
type genieRunner struct {
	chatFunc ChatFunc
}

// Run starts a chat pipeline execution and returns a channel of events.
//
// The sessionID maps to the AG-UI threadID. The message content
// is extracted from the model.Message and passed to the chatFunc.
// Events emitted by the chat pipeline (*event.Event) are forwarded
// to the returned channel. Non-event types are silently skipped.
func (r *genieRunner) Run(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
	opts ...agent.RunOption,
) (<-chan *event.Event, error) {
	eventChan := make(chan *event.Event, 100)

	// Internal channel for the chatFunc — it writes interface{} values,
	// which are predominantly *event.Event objects from the agent pipeline.
	rawChan := make(chan interface{}, 100)

	go func() {
		defer close(eventChan)

		// Start the chat pipeline in a nested goroutine.
		// The chatFunc goroutine OWNS rawChan and closes it when done.
		// This prevents the outer goroutine from closing rawChan while
		// the chatFunc is still writing to it (blind spot #1 fix).
		go func() {
			defer close(rawChan)
			if err := r.chatFunc(ctx, message.Content, rawChan); err != nil {
				// Emit error as a framework event instead of swallowing it
				// (blind spot #2 fix). The event is sent on rawChan before
				// it's closed, so the forwarding loop below will pick it up.
				select {
				case rawChan <- chatErrorToEvent(err):
				case <-ctx.Done():
				}
			}
		}()

		// Forward *event.Event values to the typed channel.
		// rawChan is closed by the chatFunc goroutine when done.
		for raw := range rawChan {
			if evt, ok := raw.(*event.Event); ok {
				select {
				case eventChan <- evt:
				case <-ctx.Done():
					return
				}
			}
			// Non-event types (e.g. UserInputMsg) are skipped —
			// they are handled by the Expert dedup layer, not the runner.
		}
	}()

	return eventChan, nil
}

// Close is a no-op — the runner doesn't own any resources beyond what
// the chatFunc manages internally via Application lifecycle.
func (r *genieRunner) Close() error {
	return nil
}

// chatErrorToEvent converts a chat pipeline error into a framework event
// with an error response, so callers see a proper error instead of silence.
func chatErrorToEvent(err error) *event.Event {
	errMsg := err.Error()
	code := "chat_error"
	return &event.Event{
		Response: &model.Response{
			Error: &model.ResponseError{
				Message: fmt.Sprintf("chat pipeline error: %s", errMsg),
				Type:    "runner_error",
				Code:    &code,
			},
		},
	}
}
