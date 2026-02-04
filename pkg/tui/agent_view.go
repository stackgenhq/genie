package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AgentView handles the visualization of the agent's execution.
type AgentView struct {
	// fullContent holds all received content (target state)
	fullContent strings.Builder
	// displayedLen tracks how many characters are currently shown (for typing animation)
	displayedLen int
	// charsPerTick controls typing speed (characters revealed per tick)
	charsPerTick int

	viewport        viewport.Model
	styles          Styles
	isThinking      bool
	thinkingMsg     string
	agentName       string
	ready           bool
	width           int
	selectedToolIdx int
}

// NewAgentView creates a new AgentView instance.
func NewAgentView(styles Styles) AgentView {
	return AgentView{
		styles:          styles,
		viewport:        viewport.New(0, 0),
		selectedToolIdx: -1,
		charsPerTick:    3, // Characters to reveal per animation tick
	}
}

// Init initializes the component.
func (m AgentView) Init() tea.Cmd {
	return nil
}

// Update handles messages for the agent view.
func (m AgentView) Update(msg tea.Msg) (AgentView, tea.Cmd) {
	switch msg := msg.(type) {
	case AgentThinkingMsg:
		m.isThinking = true
		m.thinkingMsg = msg.Message
		m.agentName = msg.AgentName
		m.updateViewport()

	case AgentStreamChunkMsg:
		if msg.Delta {
			m.fullContent.WriteString(msg.Content)
		} else {
			m.fullContent.Reset()
			m.displayedLen = 0
			m.fullContent.WriteString(msg.Content)
		}
		m.isThinking = false
		// Start or continue the typing animation
		return m, typingTickCmd()

	case TypingTickMsg:
		fullLen := m.fullContent.Len()
		if m.displayedLen < fullLen {
			// Reveal more characters
			m.displayedLen += m.charsPerTick
			if m.displayedLen > fullLen {
				m.displayedLen = fullLen
			}
			m.updateViewport()
			m.viewport.GotoBottom()
			// Continue animation if more content to reveal
			if m.displayedLen < fullLen {
				return m, typingTickCmd()
			}
		}
		return m, nil
	}

	return m, nil
}

// SetDimensions sets the dimensions of the view.
func (m *AgentView) SetDimensions(width, height int) {
	m.width = width
	m.viewport.Width = width
	m.viewport.Height = height
	m.ready = true
	m.updateViewport()
}

// updateViewport renders the content to the viewport.
func (m *AgentView) updateViewport() {
	if !m.ready {
		return
	}
	m.viewport.SetContent(m.renderContent())
}

// renderContent renders the main content area string.
func (m AgentView) renderContent() string {
	var parts []string

	// Thinking indicator
	if m.isThinking && m.thinkingMsg != "" {
		thinking := fmt.Sprintf("⟳ %s is thinking... %s", m.agentName, m.thinkingMsg)
		parts = append(parts, m.styles.Thinking.Render(thinking))
	}

	// Streaming content with typing animation
	if m.displayedLen > 0 {
		fullStr := m.fullContent.String()
		displayStr := fullStr
		if m.displayedLen < len(fullStr) {
			displayStr = fullStr[:m.displayedLen]
		}
		content := m.styles.Content.Render(displayStr)
		parts = append(parts, content)
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// View renders the component.
func (m AgentView) View() string {
	if !m.ready {
		return "Initializing..."
	}
	if m.fullContent.Len() == 0 && !m.isThinking {
		return m.styles.Content.Render("Waiting for agent to start...")
	}
	return m.viewport.View()
}

// typingTickCmd returns a command that sends a TypingTickMsg after a short delay.
func typingTickCmd() tea.Cmd {
	return tea.Tick(15*time.Millisecond, func(t time.Time) tea.Msg {
		return TypingTickMsg{}
	})
}
