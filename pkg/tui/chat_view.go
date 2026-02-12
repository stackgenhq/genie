package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type ChatMessage struct {
	Role    string // "user", "model" (Genie), "system", "tool", or "reasoning"
	Content string
}

// ChatView implementation
type ChatView struct {
	viewport  viewport.Model
	textarea  textarea.Model
	spinner   spinner.Model
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
	// initialized tracks whether SetDimensions has been called
	// This prevents rendering before proper dimensions are set, which can cause crashes
	initialized bool
	// thinkingMsg holds the current thinking context (e.g. "Reading main.tf...")
	thinkingMsg string
	// toolCalls tracks pending tool calls by ID for status updates
	toolCalls map[string]*toolCallState
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

	// Initialize animated spinner for thinking state
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#06B6D4")) // Cyan accent

	// Initialize Glamour renderer
	// We'll update the width dynamically
	r, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("ascii"),
	)

	cv := ChatView{
		textarea:  ta,
		viewport:  vp,
		spinner:   s,
		styles:    styles,
		inputChan: inputChan,
		messages:  []ChatMessage{},
		renderer:  r,
		toolCalls: make(map[string]*toolCallState),
	}

	// Welcome message will be added when SetDimensions is first called
	// to avoid rendering issues before proper dimensions are set

	return cv
}

func (m ChatView) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
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

	// Add welcome message on first initialization
	if !m.initialized {
		m.initialized = true
		welcomeMsg := `
# 🧞 Genie
### Your Intent is My Command

Welcome to **Genie Chat Mode**! 
I can help you understand and modify your generated infrastructure code.

**Try asking me:**
*   _"Explain the IAM roles created"_
*   _"Why did you choose this architecture?"_
*   _"Add a new S3 bucket"_

**Commands:** ` + "`/help`" + ` ` + "`/clear`" + `
`
		m.AddMessage("Genie", welcomeMsg)
	} else {
		m.updateViewport()
	}
}

func (m *ChatView) SetFocus(focused bool) {
	m.hasFocus = focused
	if focused {
		m.textarea.Focus()
	} else {
		m.textarea.Blur()
	}
}

// SetLoading sets the loading state of the chat view.
func (m *ChatView) SetLoading(loading bool) {
	m.isLoading = loading
	if !loading {
		m.thinkingMsg = ""
	}
	m.updateViewport()
}

// SetThinking sets a contextual thinking message (e.g. "Reading main.tf...").
// This provides more useful feedback than a generic "thinking" indicator,
// similar to how Claude Code shows what the agent is currently doing.
func (m *ChatView) SetThinking(msg string) {
	m.isLoading = true
	m.thinkingMsg = msg
	m.updateViewport()
}

// AddToolCall adds a tool call card to the chat conversation.
// Tool calls are displayed as compact styled cards showing what the agent is doing,
// similar to how Claude Code shows tool invocations inline.
func (m *ChatView) AddToolCall(msg AgentToolCallMsg) {
	// Stop generic loading since we now have specific tool info
	m.isLoading = false
	m.thinkingMsg = ""

	// Track the tool call for later status updates
	state := &toolCallState{
		ToolName:   msg.ToolName,
		Arguments:  msg.Arguments,
		ToolCallID: msg.ToolCallID,
		Status:     "running",
	}
	m.toolCalls[msg.ToolCallID] = state

	// Build a compact summary from arguments
	summary := summarizeToolArgs(msg.ToolName, msg.Arguments)
	content := fmt.Sprintf("🔧 %s %s  ⟳", msg.ToolName, summary)

	// For write operations, include diff preview immediately
	if diff := extractDiffPreview(msg.ToolName, msg.Arguments); diff != "" {
		content += "\n" + diff
	}

	m.messages = append(m.messages, ChatMessage{Role: "tool", Content: content})
	m.updateViewport()
}

// UpdateToolCall updates a tool call card with its result.
// This changes the status indicator from ⟳ (running) to ✓ (success) or ✗ (error).
func (m *ChatView) UpdateToolCall(msg AgentToolResponseMsg) {
	// Find the matching tool call message and update its status
	state, ok := m.toolCalls[msg.ToolCallID]
	if !ok {
		// Try matching by name if ID doesn't match
		for _, s := range m.toolCalls {
			if s.ToolName == msg.ToolName && s.Status == "running" {
				state = s
				ok = true
				break
			}
		}
	}
	if !ok {
		return
	}

	// Update status
	statusIcon := "✓"
	if msg.Error != nil {
		state.Status = "error"
		statusIcon = "✗"
	} else {
		state.Status = "done"
	}

	// Update the matching tool message in chat
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "tool" && strings.Contains(m.messages[i].Content, state.ToolName) && strings.Contains(m.messages[i].Content, "⟳") {
			resultSummary := summarizeToolResult(state.ToolName, msg.Response)
			summary := summarizeToolArgs(state.ToolName, state.Arguments)
			m.messages[i].Content = fmt.Sprintf("🔧 %s %s  %s  %s", state.ToolName, summary, statusIcon, resultSummary)
			break
		}
	}
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

	switch msg := msg.(type) {
	case AgentChatMessage:
		// Handle chat messages (e.g., Ctrl+C nudge notification)
		m.AddMessage(msg.Sender, msg.Message)
		return m, tiCmd

	case AgentReasoningMsg:
		m.AppendReasoning(msg.Content)
		return m, nil

	case AgentStreamChunkMsg:
		m.AppendToLastMessage(msg.Content)
		return m, nil

	case spinner.TickMsg:
		var spinnerCmd tea.Cmd
		m.spinner, spinnerCmd = m.spinner.Update(msg)
		// Re-render viewport so the spinner frame updates visually
		if m.isLoading {
			m.updateViewport()
		}
		return m, spinnerCmd

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			if m.textarea.Value() == "" {
				break
			}
			input := m.textarea.Value()

			// Check for slash commands first — handle locally without sending to agent
			if m.handleSlashCommand(input) {
				m.textarea.Reset()
				break
			}

			// Add user message to history
			m.messages = append(m.messages, ChatMessage{Role: "user", Content: input})
			// Reset streaming state for the new conversation turn
			m.isStreaming = false
			m.updateViewport()

			// Send to agent
			select {
			case m.inputChan <- input:
			default:
				// Channel full
			}

			m.textarea.Reset()

			// Immediate feedback
			m.isLoading = true
			m.thinkingMsg = ""
			m.updateViewport()
		case tea.KeyPgUp, tea.KeyPgDown, tea.KeyUp, tea.KeyDown:
			// Only allow viewport scrolling for explicit navigation keys
			m.viewport, vpCmd = m.viewport.Update(msg)
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

func (m *ChatView) AppendReasoning(text string) {
	if len(m.messages) == 0 {
		return
	}

	text = cleanContent(text)

	// Check if the last message is reasoning
	lastMsg := m.messages[len(m.messages)-1]
	if lastMsg.Role != "reasoning" {
		// Last message is not reasoning, create a new reasoning message
		m.messages = append(m.messages, ChatMessage{Role: "reasoning", Content: text})
	} else {
		// Append to the existing reasoning message
		m.messages[len(m.messages)-1].Content += text
	}
	m.updateViewport()
}

// AddErrorMessage adds an error message to the chat as a styled error bubble.
// Error messages use the "error" role and render with a red border to visually
// distinguish them from system and model messages.
func (m *ChatView) AddErrorMessage(context, errMsg string) {
	content := "❌ Error"
	if context != "" {
		content += " (" + context + ")"
	}
	content += ": " + errMsg
	m.messages = append(m.messages, ChatMessage{Role: "error", Content: content})
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
	// Guard against rendering before initialization
	if !m.initialized || m.viewport.Width <= 0 {
		return
	}

	var renderedChunks []string

	// Max width for bubbles
	maxBubbleWidth := int(float64(m.viewport.Width) * 0.7)
	if maxBubbleWidth < 20 {
		maxBubbleWidth = 20
	}

	for _, msg := range m.messages {
		var bubble string
		switch msg.Role {
		case "user":
			// User Bubble: Right Aligned, Purple
			content := lipgloss.NewStyle().Width(maxBubbleWidth).Render(msg.Content)
			bubble = m.styles.UserBubble.Render(content)
			bubble = lipgloss.NewStyle().Width(m.viewport.Width).Align(lipgloss.Right).Render(bubble)

		case "system":
			// System messages: Left Aligned, Cyan border, markdown rendered
			var rendered string
			if m.renderer != nil {
				var err error
				rendered, err = m.renderer.Render(msg.Content)
				if err != nil {
					rendered = msg.Content
				}
			} else {
				rendered = msg.Content
			}
			rendered = strings.TrimSpace(rendered)
			bubble = m.styles.SystemBubble.Width(maxBubbleWidth).Render(rendered)
			bubble = lipgloss.NewStyle().Width(m.viewport.Width).Align(lipgloss.Left).Render(bubble)

		case "reasoning":
			// Reasoning Bubble: Italic, Gray, Left Aligned
			// Apply word wrapping to reasoning content
			content := m.styles.ReasoningBubble.Width(maxBubbleWidth).Render(msg.Content)
			bubble = lipgloss.NewStyle().Width(m.viewport.Width).Align(lipgloss.Left).Render(content)

		case "error":
			// Error Bubble: Left Aligned, Red border
			content := lipgloss.NewStyle().Width(maxBubbleWidth).Render(msg.Content)
			bubble = m.styles.ErrorBubble.Width(maxBubbleWidth).Render(content)
			bubble = lipgloss.NewStyle().Width(m.viewport.Width).Align(lipgloss.Left).Render(bubble)

		case "tool":
			// Tool cards: compact styled block with color-coded diff lines
			content := m.colorDiffLines(msg.Content)
			bubble = m.styles.ToolCard.Width(maxBubbleWidth).Render(content)
			bubble = lipgloss.NewStyle().Width(m.viewport.Width).Align(lipgloss.Left).PaddingLeft(2).Render(bubble)

		default: // "model" — Genie
			// AI Bubble: Left Aligned, Gray, Markdown
			var rendered string
			var err error
			if m.renderer != nil {
				rendered, err = m.renderer.Render(msg.Content)
			} else {
				rendered = msg.Content
			}
			if err != nil {
				rendered = msg.Content
			}
			rendered = strings.TrimSpace(rendered)
			bubble = m.styles.AiBubble.Width(maxBubbleWidth).Render(rendered)
			bubble = lipgloss.NewStyle().Width(m.viewport.Width).Align(lipgloss.Left).Render(bubble)
		}
		renderedChunks = append(renderedChunks, bubble)
	}

	if m.isLoading {
		thinkingText := "Genie is thinking..."
		if m.thinkingMsg != "" {
			thinkingText = m.thinkingMsg
		}
		thinking := m.styles.Thinking.Render(fmt.Sprintf("%s %s", m.spinner.View(), thinkingText))
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

// colorDiffLines applies green/red coloring to diff lines in tool card content.
// Lines starting with "+ " are colored green (addition), "- " are red (removal).
// This makes inline diffs visually distinct and easy to scan.
func (m *ChatView) colorDiffLines(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "+ "):
			lines[i] = m.styles.DiffAdd.Render(line)
		case strings.HasPrefix(trimmed, "- "):
			lines[i] = m.styles.DiffRemove.Render(line)
		case strings.HasPrefix(trimmed, "..."):
			lines[i] = m.styles.Thinking.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

// --- contentView interface implementation ---

// HandleThinking updates the chat view to show a thinking indicator.
// Delegates to the existing SetThinking method.
func (m *ChatView) HandleThinking(msg AgentThinkingMsg) {
	m.SetThinking(msg.Message)
}

// HandleReasoning appends reasoning/thought content to the chat.
// Delegates to the existing AppendReasoning method.
func (m *ChatView) HandleReasoning(msg AgentReasoningMsg) {
	m.AppendReasoning(msg.Content)
}

// HandleStreamChunk processes a streaming text chunk in the chat view.
// Stops loading, then adds or appends the content depending on the Delta flag.
func (m *ChatView) HandleStreamChunk(msg AgentStreamChunkMsg) tea.Cmd {
	m.SetLoading(false)
	if !msg.Delta {
		m.AddMessage("Genie", msg.Content)
	} else {
		m.AppendToLastMessage(msg.Content)
	}
	return nil
}

// HandleToolCall adds a tool call card to the chat.
// Delegates to the existing AddToolCall method.
func (m *ChatView) HandleToolCall(msg AgentToolCallMsg) tea.Cmd {
	m.AddToolCall(msg)
	return nil
}

// HandleToolResponse updates a tool call card with its result.
// Delegates to the existing UpdateToolCall method.
func (m *ChatView) HandleToolResponse(msg AgentToolResponseMsg) {
	m.UpdateToolCall(msg)
}

// HandleError adds an error bubble to the chat.
// Delegates to the existing AddErrorMessage method.
func (m *ChatView) HandleError(context, errMsg string) {
	m.AddErrorMessage(context, errMsg)
}
