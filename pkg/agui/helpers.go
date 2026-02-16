package agui

import (
	"context"

	"github.com/appcd-dev/genie/pkg/logger"
)

// EmitAgentMessage sends a chat message to the UI.
func EmitAgentMessage(ctx context.Context, eventChan chan<- interface{}, sender, message string) {
	if eventChan == nil {
		return
	}
	select {
	case eventChan <- AgentChatMessage{
		Type:    EventTextMessageContent,
		Sender:  sender,
		Message: message,
	}:
	default:
		logger.GetLogger(ctx).Warn("agui event dropped (channel full), type=AgentChatMessage", "sender", sender, "message", message)
	}
}

// EmitStageProgress is a helper to emit stage progress events.
func EmitStageProgress(ctx context.Context, eventChan chan<- interface{}, stage string, stageIndex, totalStages int) {
	if eventChan == nil {
		return
	}
	progress := float64(stageIndex) / float64(totalStages)
	select {
	case eventChan <- StageProgressMsg{
		Type:        EventStepStarted,
		Stage:       stage,
		Progress:    progress,
		StageIndex:  stageIndex,
		TotalStages: totalStages,
	}:
	default:
		logger.GetLogger(ctx).Warn("agui event dropped (channel full), type=StageProgressMsg", "stage", stage, "stageIndex", stageIndex, "totalStages", totalStages)
	}
}

// EmitThinking is a helper to emit thinking/processing events.
func EmitThinking(ctx context.Context, eventChan chan<- interface{}, agentName, message string) {
	if eventChan == nil {
		return
	}
	select {
	case eventChan <- AgentThinkingMsg{
		Type:      EventRunStarted,
		AgentName: agentName,
		Message:   message,
	}:
	default:
		logger.GetLogger(ctx).Warn("agui event dropped (channel full), type=AgentThinkingMsg", "agentName", agentName, "message", message)
	}
}

// EmitCompletion is a helper to emit completion events.
func EmitCompletion(ctx context.Context, eventChan chan<- interface{}, success bool, message string, outputDir string) {
	if eventChan == nil {
		return
	}
	select {
	case eventChan <- AgentCompleteMsg{
		Type:      EventRunFinished,
		Success:   success,
		Message:   message,
		OutputDir: outputDir,
	}:
	default:
		logger.GetLogger(ctx).Warn("agui event dropped (channel full), type=AgentCompleteMsg", "success", success, "message", message, "outputDir", outputDir)
	}
}

// EmitError is a helper to emit error events.
func EmitError(ctx context.Context, eventChan chan<- interface{}, err error, context string) {
	if eventChan == nil {
		return
	}
	select {
	case eventChan <- AgentErrorMsg{
		Type:    EventRunError,
		Error:   err,
		Context: context,
	}:
	default:
		logger.GetLogger(ctx).Warn("agui event dropped (channel full), type=AgentErrorMsg", "context", context, "error", err)
	}
}
