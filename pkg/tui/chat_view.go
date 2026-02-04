package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type ChatMessage struct {
	Role    string // "user" or "model" (Genie)
	Content string
}

// ChatView implementation
type ChatView struct {
	viewport  viewport.Model
	textarea  textarea.Model
	styles    Styles
	messages  []ChatMessage
	inputChan chan<- string
	width     int
	height    int
	hasFocus  bool
	isLoading bool
	renderer  *glamour.TermRenderer
	// isStreaming tracks whether we are currently streaming a Genie response
	// This ensures delta chunks are appended to a Genie message, not a user message
	isStreaming bool
}

func NewChatView(styles Styles, inputChan chan<- string) ChatView {
	ta := textarea.New()
	ta.Placeholder = "Ask a question about your codebase..."
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 280
	ta.SetWidth(30)
	ta.SetHeight(3)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false)

	vp := viewport.New(30, 5)

	// Initialize Glamour renderer
	// We'll update the width dynamically
	r, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("ascii"),
	)

	cv := ChatView{
		textarea:  ta,
		viewport:  vp,
		styles:    styles,
		inputChan: inputChan,
		messages:  []ChatMessage{},
		renderer:  r,
	}

	// Add Welcome Banner and Message
	welcomeMsg := `
# 🧞 Genie
### Your Intent is My Command

Welcome to **Genie Chat Mode**! 
I can help you understand and modify your generated infrastructure code.

**Try asking me:**
*   _"Explain the IAM roles created"_
*   _"Why did you choose this architecture?"_
*   _"Add a new S3 bucket"_
`
	cv.AddMessage("Genie", welcomeMsg)

	return cv
}

func (m ChatView) Init() tea.Cmd {
	return textarea.Blink
}

func (m *ChatView) SetDimensions(width, height int) {
	m.width = width
	m.height = height

	// Layout:
	// Viewport takes remaining height - textarea height - margins
	taHeight := 3
	headerHeight := 2                                // Title
	vpHeight := height - taHeight - headerHeight - 2 // -2 for margins

	if vpHeight < 5 {
		vpHeight = 5
	}

	m.viewport.Width = width - 4 // Padding
	m.viewport.Height = vpHeight
	m.textarea.SetWidth(width - 4)

	// Update renderer width - Use a bit less than full width for bubbles
	bubbleWidth := int(float64(width) * 0.7)
	if bubbleWidth < 20 {
		bubbleWidth = 20
	}
	m.renderer, _ = glamour.NewTermRenderer(
		glamour.WithStandardStyle("ascii"),
		glamour.WithWordWrap(bubbleWidth),
	)
	m.updateViewport()
}

func (m *ChatView) SetFocus(focused bool) {
	m.hasFocus = focused
	if focused {
		m.textarea.Focus()
	} else {
		m.textarea.Blur()
	}
}

func (m *ChatView) SetLoading(loading bool) {
	m.isLoading = loading
	m.updateViewport()
}

func (m ChatView) Update(msg tea.Msg) (ChatView, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	if m.hasFocus {
		m.textarea, tiCmd = m.textarea.Update(msg)
	}
	m.viewport, vpCmd = m.viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			if m.textarea.Value() == "" {
				break
			}
			input := m.textarea.Value()
			// Add user message to history
			m.messages = append(m.messages, ChatMessage{Role: "user", Content: input})
			// Reset streaming state for the new conversation turn
			m.isStreaming = false
			m.updateViewport()

			// Send to agent
			// We need to do this non-blocking or in a command
			// But inputChan is buffered
			select {
			case m.inputChan <- input:
			default:
				// Channel full
			}

			m.textarea.Reset()

			// Immediate feedback
			m.isLoading = true
			m.updateViewport()
		}
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m *ChatView) AddMessage(sender, text string) {
	role := "model"
	if sender != "Genie" {
		role = "user"
	}

	text = cleanContent(text)
	m.messages = append(m.messages, ChatMessage{Role: role, Content: text})
	// Track that we've started streaming if this is a Genie message
	if role == "model" {
		m.isStreaming = true
	}
	m.updateViewport()
}

func (m *ChatView) AppendToLastMessage(text string) {
	if len(m.messages) == 0 {
		return
	}

	text = cleanContent(text)

	// Check if the last message is from Genie and we're streaming
	// If not, we need to create a new Genie message first
	lastMsg := m.messages[len(m.messages)-1]
	if lastMsg.Role != "model" {
		// Last message is from user, create a new Genie message
		m.messages = append(m.messages, ChatMessage{Role: "model", Content: text})
		m.isStreaming = true
	} else {
		// Append to the existing Genie message
		m.messages[len(m.messages)-1].Content += text
	}
	m.updateViewport()
}

// cleanContent removes generic JSON-like escapes which might confuse the renderer
func cleanContent(text string) string {
	// Simple unescaping for common cases seen in the issue
	text = strings.ReplaceAll(text, "\\n", "\n")
	text = strings.ReplaceAll(text, "\\t", "\t")
	text = strings.ReplaceAll(text, "\\\"", "\"")
	return text
}

func (m *ChatView) updateViewport() {
	var renderedChunks []string

	// Max width for bubbles
	maxBubbleWidth := int(float64(m.viewport.Width) * 0.7)
	if maxBubbleWidth < 20 {
		maxBubbleWidth = 20
	}

	for _, msg := range m.messages {
		var bubble string
		if msg.Role == "user" {
			// User Bubble: Right Aligned, Purple
			// We don't need glamour for user input usually, but we can wrap it.
			// Just plain text wrapper for now to keep it simple and clean.
			// Re-wrap manually or use lipgloss.

			content := lipgloss.NewStyle().Width(maxBubbleWidth).Render(msg.Content)
			bubble = m.styles.UserBubble.Render(content)

			// Right align the bubble in the viewport
			bubble = lipgloss.NewStyle().Width(m.viewport.Width).Align(lipgloss.Right).Render(bubble)

		} else { // Genie / Model
			// AI Bubble: Left Aligned, Gray, Markdown
			var rendered string
			var err error
			if m.renderer != nil {
				rendered, err = m.renderer.Render(msg.Content)
			} else {
				rendered = msg.Content
			}

			if err != nil {
				rendered = msg.Content // Fallback
			}
			// Glamour adds margins we might not want inside a bubble, but let's try.
			// We need to trim potentially excessive newlines from glamour
			rendered = strings.TrimSpace(rendered)

			bubble = m.styles.AiBubble.Width(maxBubbleWidth).Render(rendered)

			// Left align (default)
			bubble = lipgloss.NewStyle().Width(m.viewport.Width).Align(lipgloss.Left).Render(bubble)
		}
		renderedChunks = append(renderedChunks, bubble)
	}

	if m.isLoading {
		thinking := m.styles.Thinking.Render("⟳ Genie is thinking...")
		renderedChunks = append(renderedChunks, lipgloss.NewStyle().Width(m.viewport.Width).Align(lipgloss.Left).PaddingLeft(2).Render(thinking))
	}

	m.viewport.SetContent(strings.Join(renderedChunks, "\n"))
	m.viewport.GotoBottom()
}

func (m ChatView) View() string {
	var panelStyle, titleStyle lipgloss.Style
	if m.hasFocus {
		panelStyle = m.styles.FocusedBorder
		titleStyle = m.styles.FocusedTitle
	} else {
		panelStyle = m.styles.DimBorder
		titleStyle = m.styles.DimTitle
	}

	content := fmt.Sprintf(
		"%s\n%s\n\n%s",
		titleStyle.Render("💬 Talk to your Codebase"),
		m.viewport.View(),
		m.textarea.View(),
	)

	return panelStyle.
		Width(m.width - 2). // Adjust for borders
		Render(content)
}
