package tui

import (
	"context"
	"fmt"
	"log/slog"

	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// eventChannelClosedMsg is sent when the event channel is closed.
type eventChannelClosedMsg struct{}

// waitForEvent waits for the next event from the event channel.
// This returns a Bubble Tea command that will send the event as a message.
func waitForEvent(eventChan <-chan interface{}) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-eventChan
		if !ok {
			return eventChannelClosedMsg{}
		}
		// Event should already be a tea.Msg type (one of our event types)
		if msg, ok := event.(tea.Msg); ok {
			return msg
		}
		// If not, return as-is and let Update handle it
		return event
	}
}

// tickCmd returns a command that sends a TickMsg after 1 second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg{Time: t}
	})
}

// formatElapsedTime formats the elapsed duration in a human-readable format.
func formatElapsedTime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%02ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%02dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%02dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

// Example shows how to integrate the TUI with a Cobra command.
// This demonstrates the complete flow from command execution to TUI display.
//
// Usage in your Cobra command's RunE function:
//
//	RunE: func(cmd *cobra.Command, args []string) error {
//	    return tui.RunGrantWithTUI(cmd.Context(), granterService, opts)
//	}
func Example_CobraIntegration() {
	// This example shows the pattern - see RunGrantWithTUI for actual implementation
}

// RunGrantWithTUI is a helper function that demonstrates how to use the TUI
// with the grant command in a Cobra CLI context.
//
// This function exists to provide a complete, working example of TUI integration.
// Without this helper, developers would need to manually wire up the event channels,
// adapter, and goroutine coordination.
func RunGrantWithTUI(
	ctx context.Context,
	runnerFunc func(ctx context.Context, eventChan chan<- interface{}, inputChan <-chan string) error,
) error {
	// Create buffered channels for event pipeline:
	// rawEventChan: receives raw events from agent (event.Event, custom events, etc.)
	// tuiEventChan: receives converted TUI messages from adapter
	rawEventChan := make(chan interface{}, 100)
	tuiEventChan := make(chan interface{}, 100)
	// inputChan: sends user input from TUI to agent
	inputChan := make(chan string, 10)

	// Create event adapter to convert raw events to TUI messages
	adapter := NewEventAdapter("Genie")

	// Create TUI model that receives converted messages
	model := NewModel(tuiEventChan, inputChan)

	// Create Bubble Tea program with options
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // Use alternate screen buffer (cleaner exit)
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	// Start event adapter in goroutine to convert events
	go adapter.Start(rawEventChan, tuiEventChan)

	// Start the agent runner
	agentErrChan := make(chan error, 1)
	go func() {
		defer close(rawEventChan) // Close raw event channel when agent finishes
		agentErrChan <- runnerFunc(ctx, rawEventChan, inputChan)
	}()

	// Run TUI (blocks until completion or Ctrl+C)
	finalModel, err := program.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	// Check if agent encountered an error
	select {
	case agentErr := <-agentErrChan:
		if agentErr != nil {
			return fmt.Errorf("agent error: %w", agentErr)
		}
	default:
		// Agent still running or already finished
	}

	// Check final model state
	m := finalModel.(Model)
	if m.state.Err != nil {
		return fmt.Errorf("operation failed: %w", m.state.Err)
	}

	return nil
}

// EmitAgentMessage sends a chat message to the UI
func EmitAgentMessage(eventChan chan<- interface{}, sender, message string) {
	select {
	case eventChan <- AgentChatMessage{
		Sender:  sender,
		Message: message,
	}:
	default:
		// Channel full, skip
	}
}

// RunExpertWithTUI demonstrates how to run an expert with TUI integration.
// This is a more specific example for the expert package.
//
// Example usage in grant command:
//
//	err := tui.RunExpertWithTUI(ctx, "Architect", func(ctx context.Context, eventChan chan<- interface{}) error {
//	    response, err := expert.Do(ctx, expert.Request{
//	        Message: "Generate architecture",
//	        EventChannel: eventChan,
//	    })
//	    return err
//	})
func RunExpertWithTUI(
	ctx context.Context,
	expertName string,
	expertFunc func(ctx context.Context, eventChan chan<- interface{}) error,
) error {
	// Adapter to match the expected signature
	return RunGrantWithTUI(ctx, func(ctx context.Context, eventChan chan<- interface{}, inputChan <-chan string) error {
		return expertFunc(ctx, eventChan)
	})
}

// EmitStageProgress is a helper to emit stage progress events.
// Use this in your grant workflow to show progress through stages.
//
// Example:
//
//	tui.EmitStageProgress(eventChan, "Analyzing", 0, 3)
//	// ... do analysis work ...
//	tui.EmitStageProgress(eventChan, "Architecting", 1, 3)
//	// ... do architecture work ...
//	tui.EmitStageProgress(eventChan, "Generating", 2, 3)
func EmitStageProgress(eventChan chan<- interface{}, stage string, stageIndex, totalStages int) {
	progress := float64(stageIndex) / float64(totalStages)
	select {
	case eventChan <- StageProgressMsg{
		Stage:       stage,
		Progress:    progress,
		StageIndex:  stageIndex,
		TotalStages: totalStages,
	}:
	default:
		// Channel full, skip
	}
}

// EmitThinking is a helper to emit thinking/processing events.
// Use this to show when the agent is working on something.
//
// Example:
//
//	tui.EmitThinking(eventChan, "Architect", "Analyzing requirements...")
func EmitThinking(eventChan chan<- interface{}, agentName, message string) {
	select {
	case eventChan <- AgentThinkingMsg{
		AgentName: agentName,
		Message:   message,
	}:
	default:
		// Channel full, skip
	}
}

// EmitCompletion is a helper to emit completion events.
// Use this when the operation completes successfully.
//
// Example:
//
//	tui.EmitCompletion(eventChan, true, "Infrastructure generated successfully!", "/tmp/output")
func EmitCompletion(eventChan chan<- interface{}, success bool, message string, outputDir string) {
	select {
	case eventChan <- AgentCompleteMsg{
		Success:   success,
		Message:   message,
		OutputDir: outputDir,
	}:
	default:
		// Channel full, skip
	}
}

// EmitError is a helper to emit error events.
// Use this when an error occurs during processing.
//
// Example:
//
//	tui.EmitError(eventChan, err, "during architecture generation")
func EmitError(eventChan chan<- interface{}, err error, context string) {
	select {
	case eventChan <- AgentErrorMsg{
		Error:   err,
		Context: context,
	}:
	default:
		// Channel full, skip
	}
}

// SetupTUILogger configures the application's slog logger to pipe all logs to the TUI.
// This should be called before starting the TUI to enable automatic log streaming.
//
// Example usage:
//
// eventChan := make(chan interface{}, 100)
// tuiHandler := tui.SetupTUILogger(eventChan, slog.LevelInfo)
// logger.SetLogHandler(tuiHandler)
// // Now all logger.GetLogger(ctx).Info/Warn/Error calls will appear in the TUI
//
// This is the recommended approach for log integration - it automatically captures
// all application logs without requiring manual EmitLog calls.
func SetupTUILogger(eventChan chan<- interface{}, level slog.Level) *TUIHandler {
	return NewTUIHandler(eventChan, level)
}
