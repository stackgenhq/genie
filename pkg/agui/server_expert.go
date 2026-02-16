package agui

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/genie/pkg/logger"
)

// NewChatHandlerFromCodeOwner creates a ChatHandler that bridges the AG-UI
// server to the existing codeOwner.Chat() pipeline.
//
// The expert.Do() method converts raw *event.Event objects to TUI messages
// (AgentStreamChunkMsg, TextMessageStartMsg, etc.) via its own EventAdapter
// before writing them to the event channel. The ReAcTree runs multiple stages,
// each producing the same text content — causing duplicated output.
//
// This function inserts a dedup filter between the agent and the SSE stream:
// after the first TEXT_MESSAGE_END, all subsequent text message events are
// suppressed while tool calls and stage progress events pass through.
func NewChatHandlerFromCodeOwner(
	resume string,
	chatFunc func(ctx context.Context, message string, agentsMessage chan<- interface{}) error,
) Expert {
	return expert{
		resume:   resume,
		chatFunc: chatFunc,
	}
}

type expert struct {
	resume   string
	chatFunc func(ctx context.Context, message string, agentsMessage chan<- interface{}) error
}

func (e expert) Resume() string {
	return e.resume
}

func (e expert) Handle(ctx context.Context, req ChatRequest) {
	// Emit RUN_STARTED
	req.EventChan <- AgentThinkingMsg{
		Type:      EventRunStarted,
		AgentName: "Genie",
		Message:   "Processing your request...",
	}

	// Create a raw event channel for the agent.
	// expert.Do() writes TUI messages here (not raw *event.Event).
	rawEventChan := make(chan interface{}, 100)

	// Dedup filter: reads TUI messages from rawEventChan, suppresses
	// multi-stage text replays, and forwards everything else.
	//
	// The trpc-agent library streams individual content deltas during LLM
	// generation and then replays the full accumulated text as a single
	// chunk. The ReAcTree may also run the same expert across stages which
	// can trigger another replay of all content.
	//
	// We suppress duplicates by tracking per-messageID cumulative content.
	// If a chunk's text is already contained in what we've sent, it's a
	// replay and gets dropped.
	converterDone := make(chan struct{})
	go func() {
		defer close(converterDone)

		sentStart := make(map[string]bool)     // messageId → true after first START
		sentContent := make(map[string]string) // messageId → all content sent so far

		for raw := range rawEventChan {
			select {
			case <-ctx.Done():
				return
			default:
			}

			switch evt := raw.(type) {
			// ── Text message lifecycle: cumulative dedup ──
			case TextMessageStartMsg:
				if sentStart[evt.MessageID] {
					continue // suppress duplicate START for same messageId
				}
				sentStart[evt.MessageID] = true
				req.EventChan <- evt

			case AgentStreamChunkMsg:
				prior := sentContent[evt.MessageID]
				// The final accumulated replay contains text we've
				// already streamed as individual deltas. Detect this by
				// checking if the prior content already starts with the
				// incoming chunk (replay of earlier deltas) or if the
				// incoming chunk is longer than a typical delta and is a
				// prefix of or equal to the accumulated text.
				if len(evt.Content) > 0 && strings.HasPrefix(prior, evt.Content) {
					continue // already sent — suppress replay
				}
				sentContent[evt.MessageID] = prior + evt.Content
				req.EventChan <- evt

			case TextMessageEndMsg:
				req.EventChan <- evt

			case AgentReasoningMsg:
				req.EventChan <- evt

			// ── Tool events: always pass through ──
			case AgentToolCallMsg:
				req.EventChan <- evt
			case ToolCallArgsMsg:
				req.EventChan <- evt
			case ToolCallEndMsg:
				req.EventChan <- evt
			case AgentToolResponseMsg:
				req.EventChan <- evt

			// ── Lifecycle/progress events: always pass through ──
			case StageProgressMsg:
				req.EventChan <- evt
			case AgentThinkingMsg:
				req.EventChan <- evt
			case AgentErrorMsg:
				req.EventChan <- evt
			case AgentCompleteMsg:
				req.EventChan <- evt
			case AgentChatMessage:
				req.EventChan <- evt
			case LogMsg:
				req.EventChan <- evt

			// ── HITL approval events: always pass through ──
			case ToolApprovalRequestMsg:
				req.EventChan <- evt

			default:
				logger.GetLogger(ctx).Warn("agui: skipping unknown raw event", "type", fmt.Sprintf("%T", raw))
			}
		}
	}()

	// Run the agent — it writes TUI messages to rawEventChan.
	// Inject ThreadID/RunID into context so the toolwrap.Wrapper can access them
	// for HITL approval requests without threading through every intermediate struct.
	agentCtx := context.WithValue(ctx, ctxKeyThreadID, req.ThreadID)
	agentCtx = context.WithValue(agentCtx, ctxKeyRunID, req.RunID)
	// Store rawEventChan in context so sub-agent tool wrappers can emit
	// HITL approval events back to the UI even when they don't have a
	// direct reference to the event channel.
	agentCtx = WithEventChan(agentCtx, rawEventChan)
	logger.GetLogger(ctx).Info("agui: invoking chatFunc",
		"threadID", req.ThreadID, "runID", req.RunID,
		"messageLen", len(req.Message))
	if err := e.chatFunc(agentCtx, req.Message, rawEventChan); err != nil {
		req.EventChan <- AgentErrorMsg{
			Type:    EventRunError,
			Error:   err,
			Context: "while processing chat message",
		}
	}

	// Close raw channel to signal converter to finish, wait for it to drain.
	close(rawEventChan)
	<-converterDone

	// Emit RUN_FINISHED
	req.EventChan <- AgentCompleteMsg{
		Type:    EventRunFinished,
		Success: true,
		Message: "Request completed",
	}
}
