package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// RunnerFunc is a function that runs the agent workflow and emits events to the event channel.
// The function should close the event channel when it's done.
type RunnerFunc func(ctx context.Context, eventChan chan<- interface{}, inputChan <-chan string) error

// RunWithTUI runs the given runner function with the TUI.
// This method exists to coordinate the agent runner goroutine with the Bubble Tea TUI.
// Without this coordination, the agent and TUI would run independently without communication.
//
// The runner function should:
//   - Accept a context for cancellation
//   - Accept an event channel to emit events
//   - Close the event channel when done
//   - Return an error if the operation fails
//
// Example usage:
//
//	err := tui.RunWithTUI(ctx, func(ctx context.Context, eventChan chan<- interface{}) error {
//	    defer close(eventChan)
//	    // Run your agent workflow here
//	    // Emit events to eventChan
//	    return nil
//	})
func RunWithTUI(ctx context.Context, runnerFunc RunnerFunc) error {
	// Create event channel for communication between agent and TUI
	eventChan := make(chan interface{}, 100)
	// Create input channel for TUI -> Agent communication
	inputChan := make(chan string, 10)

	// Create TUI model
	model := NewModel(eventChan, inputChan)

	// Create Bubble Tea program
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // Use alternate screen buffer
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	// Start agent runner in goroutine
	agentErrChan := make(chan error, 1)
	go func() {
		agentErrChan <- runnerFunc(ctx, eventChan, inputChan)
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
