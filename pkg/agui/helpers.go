package agui

// EmitAgentMessage sends a chat message to the UI.
func EmitAgentMessage(eventChan chan<- interface{}, sender, message string) {
	if eventChan == nil {
		return
	}
	eventChan <- AgentChatMessage{
		Type:    EventTextMessageContent,
		Sender:  sender,
		Message: message,
	}
}

// EmitStageProgress is a helper to emit stage progress events.
func EmitStageProgress(eventChan chan<- interface{}, stage string, stageIndex, totalStages int) {
	if eventChan == nil {
		return
	}
	progress := float64(stageIndex) / float64(totalStages)
	eventChan <- StageProgressMsg{
		Type:        EventStepStarted,
		Stage:       stage,
		Progress:    progress,
		StageIndex:  stageIndex,
		TotalStages: totalStages,
	}
}

// EmitThinking is a helper to emit thinking/processing events.
func EmitThinking(eventChan chan<- interface{}, agentName, message string) {
	if eventChan == nil {
		return
	}
	eventChan <- AgentThinkingMsg{
		Type:      EventRunStarted,
		AgentName: agentName,
		Message:   message,
	}
}

// EmitCompletion is a helper to emit completion events.
func EmitCompletion(eventChan chan<- interface{}, success bool, message string, outputDir string) {
	if eventChan == nil {
		return
	}
	eventChan <- AgentCompleteMsg{
		Type:      EventRunFinished,
		Success:   success,
		Message:   message,
		OutputDir: outputDir,
	}
}

// EmitError is a helper to emit error events.
func EmitError(eventChan chan<- interface{}, err error, context string) {
	if eventChan == nil {
		return
	}
	eventChan <- AgentErrorMsg{
		Type:    EventRunError,
		Error:   err,
		Context: context,
	}
}
