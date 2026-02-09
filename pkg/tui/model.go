package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type FocusArea int

const (
	FocusChat FocusArea = iota
	FocusLogs
)

// ModelState holds the dynamic state of the application.
type ModelState struct {
	CurrentStage  string
	StageIndex    int
	TotalStages   int
	Progress      float64
	Completed     bool
	Success       bool
	CompletionMsg string
	Err           error
	ErrorContext  string
}

// Model represents the TUI application state following Bubble Tea's MVU pattern.
// This model maintains all state needed to render the interactive terminal UI.
type Model struct {
	// eventChan receives events from the agent runner
	eventChan <-chan interface{}
	// inputChan sends user input to the agent runner
	inputChan chan<- string

	// Components
	agentView AgentView
	chatView  ChatView
	logView   LogView

	// focus state
	focus FocusArea

	// Application State
	state ModelState

	// Time tracking
	startTime   time.Time
	elapsedTime time.Duration

	// UI dimensions
	width  int
	height int

	// Styles
	styles Styles

	// Exit state
	quitting    bool
	quitAttempt bool
	// channelClosed tracks if the event channel has been closed
	channelClosed bool
}

type resetQuitAttemptMsg struct{}

// NewModel creates a new TUI model with the given event channel.
// The event channel should receive events from the agent runner.
// The model will listen to this channel and update its state accordingly.
func NewModel(eventChan <-chan interface{}, inputChan chan<- string) Model {
	styles := DefaultStyles()
	return Model{
		eventChan: eventChan,
		inputChan: inputChan,
		agentView: NewAgentView(styles),
		chatView:  NewChatView(styles, inputChan),
		logView:   NewLogView(styles),
		state: ModelState{
			TotalStages: 4, // Analyze, Architect, Generate, Complete
		},
		styles:    styles,
		startTime: time.Now(),
	}
}

// Init initializes the model and returns the initial command.
// This is called once when the Bubble Tea program starts.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		waitForEvent(m.eventChan),
		tickCmd(), // Start the elapsed time ticker
		m.agentView.Init(),
		m.chatView.Init(),
		m.logView.Init(),
	)
}

// Update handles incoming messages and updates the model state.
// This is the core of the MVU pattern - all state changes happen here.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutComponents()
		return m, nil

		// Update case for Tab handling
	// ...
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.quitAttempt {
				m.quitting = true
				return m, tea.Quit
			}
			m.quitAttempt = true

			// Nudge user via Chat Message
			nudgeCmd := func() tea.Msg {
				return AgentChatMessage{
					Sender:  "Genie",
					Message: "Press Ctrl+C again to put Genie back in the bottle 🧞",
				}
			}

			// Reset quit attempt after 2 seconds
			resetCmd := tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return resetQuitAttemptMsg{}
			})

			return m, tea.Batch(nudgeCmd, resetCmd)

		case "tab":
			// Toggle focus between chat and logs in completed state
			if m.state.Completed {
				if m.focus == FocusChat {
					m.focus = FocusLogs
					m.chatView.SetFocus(false)
					m.logView.SetFocus(true)
				} else {
					m.focus = FocusChat
					m.chatView.SetFocus(true)
					m.logView.SetFocus(false)
				}
				return m, nil
			}
			// Ignore tab key when state is not completed to avoid unintended propagation
			return m, nil
		}

		// Pass keys to active component
		if m.state.Completed {
			var cmd tea.Cmd
			switch msg.Type {
			case tea.KeyPgUp, tea.KeyPgDown, tea.KeyUp, tea.KeyDown:
				// Route scroll keys to focused pane
				if m.focus == FocusLogs {
					m.logView, cmd = m.logView.Update(msg)
				} else {
					m.chatView, cmd = m.chatView.Update(msg)
				}
				return m, cmd
			default:
				// Other keys go to chat (for typing)
				m.chatView, cmd = m.chatView.Update(msg)
				return m, cmd
			}
		}

		// Map global navigation keys to specific components
		switch msg.String() {
		case "k", "ctrl+u":
			// Log scrolling
			var cmd tea.Cmd
			m.logView, cmd = m.logView.Update(msg)
			cmds = append(cmds, cmd)
		case "j", "ctrl+d":
			// Log scrolling
			var cmd tea.Cmd
			m.logView, cmd = m.logView.Update(msg)
			cmds = append(cmds, cmd)
		default:
			// Default to agent view scrolling
			var cmd tea.Cmd
			m.agentView, cmd = m.agentView.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case resetQuitAttemptMsg:
		m.quitAttempt = false
		return m, nil

	case AgentChatMessage:
		var cmd tea.Cmd
		m.chatView, cmd = m.chatView.Update(msg)
		return m, cmd

	case AgentThinkingMsg:
		if m.state.Completed {
			m.chatView.SetLoading(true)
		} else {
			m.agentView.isThinking = true
			m.agentView.thinkingMsg = msg.Message
			m.agentView.agentName = msg.AgentName
			m.agentView.updateViewport()
		}
		return m, waitForEvent(m.eventChan)

	case TypingTickMsg:
		if !m.state.Completed {
			var cmd tea.Cmd
			m.agentView, cmd = m.agentView.Update(msg)
			return m, cmd
		}
		return m, nil

	case AgentStreamChunkMsg:
		if m.state.Completed {
			m.chatView.SetLoading(false)
			if !msg.Delta {
				m.chatView.AddMessage("Genie", msg.Content)
			} else {
				m.chatView.AppendToLastMessage(msg.Content)
			}
		} else {
			var cmd tea.Cmd
			m.agentView, cmd = m.agentView.Update(msg)
			cmds = append(cmds, cmd)
		}
		// Always continue listening for more events
		cmds = append(cmds, waitForEvent(m.eventChan))
		return m, tea.Batch(cmds...)

	case AgentToolCallMsg:
		if m.state.Completed {
			m.chatView.AddMessage("Genie", "🔧 Calling "+msg.ToolName)
		} else {
			var cmd tea.Cmd
			m.agentView, cmd = m.agentView.Update(msg)
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, waitForEvent(m.eventChan))
		return m, tea.Batch(cmds...)

	case AgentToolResponseMsg:
		if !m.state.Completed {
			m.agentView, _ = m.agentView.Update(msg)
		}
		return m, waitForEvent(m.eventChan)

	case StageProgressMsg:
		m.state.CurrentStage = msg.Stage
		m.state.StageIndex = msg.StageIndex
		m.state.TotalStages = msg.TotalStages
		m.state.Progress = msg.Progress
		return m, waitForEvent(m.eventChan)

	case AgentCompleteMsg:
		m.state.Completed = true
		m.state.Success = msg.Success
		m.state.CompletionMsg = msg.Message

		// Also update agent view to stop thinking
		m.agentView.isThinking = false
		m.agentView.updateViewport()

		// Ensure chat is focused when completed
		m.focus = FocusChat
		m.chatView.SetFocus(true)

		m.layoutComponents() // Re-layout to set dimensions for chat and browser

		return m, tea.Batch(cmds...)

	case AgentErrorMsg:
		m.state.Err = msg.Error
		m.state.ErrorContext = msg.Context
		m.agentView.isThinking = false
		m.chatView.SetLoading(false)
		return m, nil

	case LogMsg:
		entry := LogEntry{
			Timestamp: time.Now(),
			Level:     msg.Level,
			Message:   msg.Message,
			Source:    msg.Source,
		}
		m.logView.AddLog(entry)
		return m, waitForEvent(m.eventChan)

	case TickMsg:
		m.elapsedTime = time.Since(m.startTime)
		// Only schedule next tick if not completed
		var nextTick tea.Cmd
		if !m.state.Completed {
			nextTick = tickCmd()
		}

		// Only wait for more events if channel is open
		var nextEvent tea.Cmd
		if !m.channelClosed {
			nextEvent = waitForEvent(m.eventChan)
		}

		if nextTick != nil && nextEvent != nil {
			return m, tea.Batch(nextEvent, nextTick)
		} else if nextTick != nil {
			return m, nextTick
		} else if nextEvent != nil {
			return m, nextEvent
		}
		return m, nil

	case eventChannelClosedMsg:
		m.channelClosed = true
		if !m.state.Completed {
			m.state.Completed = true
			m.state.Success = m.state.Err == nil
			m.layoutComponents() // Re-layout
		}

		// If successfully completed, keep TUI open to show results (e.g. file browser)
		if m.state.Success {
			return m, nil
		}
		return m, tea.Quit

	default:
		// Forward any other messages (like filepicker IO results) to the active component
		if m.state.Completed {
			var cmd tea.Cmd
			m.chatView, cmd = m.chatView.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// layoutComponents calculates and sets the dimensions for all components
func (m *Model) layoutComponents() {
	// Guard: skip layout if window size hasn't been received yet
	if m.width == 0 || m.height == 0 {
		return
	}

	headerHeight := 3
	progressHeight := 2
	footerHeight := 2

	// Better logic:
	// Logs get 1/3 of remaining space or min 5 lines
	// Content/AgentView gets the rest

	availableHeight := m.height - headerHeight - progressHeight - footerHeight
	logHeight := availableHeight / 3
	if logHeight < 5 {
		logHeight = 5
	}

	agentViewHeight := availableHeight - logHeight
	if agentViewHeight < 5 {
		agentViewHeight = 5
		// Steal from logs if really crunched
		logHeight = availableHeight - agentViewHeight
	}

	// If completed, 2-column layout: chat (75%) + logs (25%)
	if m.state.Completed {
		chatWidth := int(float64(m.width) * 0.5)
		logWidth := m.width - chatWidth
		m.chatView.SetDimensions(chatWidth, availableHeight)
		m.logView.SetDimensions(logWidth-4, availableHeight) // -4 for borders
	} else {
		// Normal mode: split between agent view and logs vertically
		m.logView.SetDimensions(m.width-4, logHeight) // -4 for borders
		m.agentView.SetDimensions(m.width, agentViewHeight)
	}
}

// View renders the current model state to a string for terminal display.
// This is called after every Update to refresh the UI.
func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	var sections []string

	// Header
	sections = append(sections, m.renderHeader())

	// Progress
	if !m.state.Completed {
		sections = append(sections, m.renderProgress())
	}

	// Main Content Area
	if m.state.Err != nil {
		sections = append(sections, m.renderError())
		// Show logs even on error for debugging
		sections = append(sections, m.logView.View())
	} else if m.state.Completed {
		sections = append(sections, m.renderCompletion())
		// 2-column layout: chat on left (75%), logs on right (25%)
		chatAndLogs := lipgloss.JoinHorizontal(lipgloss.Top,
			m.chatView.View(),
			m.logView.View(),
		)
		sections = append(sections, chatAndLogs)
	} else {
		sections = append(sections, m.agentView.View())
		// Logs only shown if not completed
		sections = append(sections, m.logView.View())
	}

	// Footer
	sections = append(sections, m.renderFooter())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderHeader renders the application header with elapsed time.
func (m Model) renderHeader() string {
	title := "🧞 genie - Your Intent is My Command    Built @ Stackgen with ❤️"
	if m.elapsedTime > 0 {
		elapsed := formatElapsedTime(m.elapsedTime)
		title += fmt.Sprintf("    ⏱️  Elapsed: %s", elapsed)
	}

	header := m.styles.Header.Render(title)
	return header
}

// renderProgress renders the multi-stage progress indicator.
func (m Model) renderProgress() string {
	stages := []string{"Analyzing", "Architecting", "Generating", "Complete"}
	var parts []string

	for i, stage := range stages {
		var stageStr string
		if i < m.state.StageIndex {
			stageStr = m.styles.StageComplete.Render("✓ " + stage)
		} else if i == m.state.StageIndex {
			if m.agentView.isThinking {
				stageStr = m.styles.StageCurrent.Render("⟳ " + stage)
			} else {
				stageStr = m.styles.StageCurrent.Render("▶ " + stage)
			}
		} else {
			stageStr = m.styles.StagePending.Render("○ " + stage)
		}
		parts = append(parts, stageStr)
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

// renderError renders the error state.
func (m Model) renderError() string {
	errMsg := "❌ Error"
	if m.state.ErrorContext != "" {
		errMsg += " (" + m.state.ErrorContext + ")"
	}
	errMsg += ": " + m.state.Err.Error()
	return m.styles.Error.Render(errMsg)
}

// renderCompletion renders the completion state.
func (m Model) renderCompletion() string {
	if m.state.Success {
		msg := "✅ " + m.state.CompletionMsg
		if m.state.CompletionMsg == "" {
			msg = "✅ Your infrastructure is ready!"
		}
		return m.styles.Success.Render(msg)
	}
	// Fallback if success is false but err is nil (shouldn't happen)
	return m.styles.Error.Render("❌ Operation failed")
}

// renderFooter renders the footer with help text.
func (m Model) renderFooter() string {
	help := "Press Ctrl+C to cancel"

	if m.state.Completed {
		help += " • Enter: Send"
	} else {
		help += " • PgUp/PgDn/Space/b to scroll content • Shift+j/k for logs"
	}

	return m.styles.Footer.Render(help)
}
