package agui

import (
	"context"
)

// EmitAgentMessage sends a chat message to the UI via the event bus.
func EmitAgentMessage(ctx context.Context, sender, message string) {
	Emit(ctx, AgentChatMessage{
		Type:    EventTextMessageContent,
		Sender:  sender,
		Message: message,
	})
}

// EmitStageProgress is a helper to emit stage progress events.
func EmitStageProgress(ctx context.Context, stage string, stageIndex, totalStages int) {
	progress := float64(stageIndex) / float64(totalStages)
	Emit(ctx, StageProgressMsg{
		Type:        EventStepStarted,
		Stage:       stage,
		Progress:    progress,
		StageIndex:  stageIndex,
		TotalStages: totalStages,
	})
}

// EmitThinking is a helper to emit thinking/processing events.
func EmitThinking(ctx context.Context, agentName, message string) {
	Emit(ctx, AgentThinkingMsg{
		Type:      EventRunStarted,
		AgentName: agentName,
		Message:   message,
	})
}

// EmitCompletion is a helper to emit completion events.
func EmitCompletion(ctx context.Context, success bool, message string, outputDir string) {
	Emit(ctx, AgentCompleteMsg{
		Type:      EventRunFinished,
		Success:   success,
		Message:   message,
		OutputDir: outputDir,
	})
}

// EmitError is a helper to emit error events.
func EmitError(ctx context.Context, err error, context_ string) {
	Emit(ctx, AgentErrorMsg{
		Type:    EventRunError,
		Error:   err,
		Context: context_,
	})
}
