package agui

import (
	"context"

	aguitypes "github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/orchestrator/orchestratorcontext"
)

// NewChatHandler creates a ChatHandler that bridges the AG-UI
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
func NewChatHandler(
	resumeFunc func(ctx context.Context) string,
	chatFunc func(ctx context.Context, message string, agentsMessage chan<- interface{}) error,
) Expert {
	return serverExpert{
		resumeFunc: resumeFunc,
		chatFunc:   chatFunc,
	}
}

type serverExpert struct {
	resumeFunc func(ctx context.Context) string
	chatFunc   func(ctx context.Context, message string, agentsMessage chan<- interface{}) error
}

func (e serverExpert) Resume(ctx context.Context) string {
	if e.resumeFunc == nil {
		return ""
	}
	return e.resumeFunc(ctx)
}

func (e serverExpert) Handle(ctx context.Context, req ChatRequest) {
	// Emit RUN_STARTED
	req.EventChan <- aguitypes.AgentThinkingMsg{
		Type:      aguitypes.EventRunStarted,
		AgentName: orchestratorcontext.AgentFromContext(ctx).Name,
		Message:   "Processing your request...",
	}

	// Run the agent — it writes TUI messages to rawEventChan.
	// Inject ThreadID/RunID into context so the toolwrap.Wrapper can access them
	// for HITL approval requests without threading through every intermediate struct.
	ctx = aguitypes.WithThreadID(ctx, req.ThreadID)
	logger.GetLogger(ctx).Info("agui: invoking chatFunc",
		"threadID", req.ThreadID, "runID", req.RunID,
		"messageLen", len(req.Message))
	err := e.chatFunc(aguitypes.WithRunID(ctx, req.RunID), req.Message, req.EventChan)
	if err != nil {
		req.EventChan <- aguitypes.AgentErrorMsg{
			Type:    aguitypes.EventRunError,
			Error:   err,
			Context: "while processing chat message",
		}
	}

	// Emit RUN_FINISHED
	req.EventChan <- aguitypes.AgentCompleteMsg{
		Type:    aguitypes.EventRunFinished,
		Success: true,
		Message: "Request completed",
	}
}
