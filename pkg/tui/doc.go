// Package tui provides a terminal user interface for the genie CLI using Bubble Tea.
//
// This package implements an interactive TUI that displays real-time progress during
// the agentic IaC generation process. It integrates with trpc-agent-go's event-driven
// architecture to provide visual feedback for:
//   - Streaming LLM responses
//   - Tool call execution
//   - Multi-stage progress tracking
//   - Error handling
//
// Architecture:
//
// The TUI follows Bubble Tea's Model-View-Update (MVU) pattern:
//   - Model: Maintains application state (current stage, content buffer, tool calls)
//   - Update: Handles incoming events and updates state
//   - View: Renders the current state to terminal output
//
// Event Flow:
//
//	┌──────────────┐    Events    ┌─────────────┐    tea.Msg    ┌──────────┐
//	│ trpc-agent-go│─────────────▶│Event Adapter│──────────────▶│Bubble Tea│
//	│   Runner     │              │  (Channel)  │               │   TUI    │
//	└──────────────┘              └─────────────┘               └──────────┘
//	     (Goroutine)                                              (Main Loop)
//
// The event adapter runs in a goroutine, listening to trpc-agent-go events and
// converting them to Bubble Tea messages that are sent to the TUI's Update function.
//
// Usage:
//
//	// Create event channel
//	eventChan := make(chan interface{}, 100)
//
//	// Create and start TUI
//	model := tui.NewModel(eventChan)
//	program := tea.NewProgram(model)
//
//	// Start agent in goroutine
//	go func() {
//	    defer close(eventChan)
//	    // Run agent, emit events to eventChan
//	}()
//
//	// Run TUI (blocks until completion or Ctrl+C)
//	if _, err := program.Run(); err != nil {
//	    return err
//	}
package tui
