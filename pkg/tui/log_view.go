package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LogView handles the display of system logs.
type LogView struct {
	logs     []LogEntry
	viewport viewport.Model
	styles   Styles
	ready    bool
	height   int
	width    int
	hasFocus bool
}

// NewLogView creates a new LogView instance.
func NewLogView(styles Styles) LogView {
	return LogView{
		logs:     make([]LogEntry, 0),
		styles:   styles,
		viewport: viewport.New(0, 0),
	}
}

// Init initializes the component.
func (m LogView) Init() tea.Cmd {
	return nil
}

// Update handles messages for the log view.
func (m LogView) Update(msg tea.Msg) (LogView, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// SetFocus sets the focus state of the log view.
func (m *LogView) SetFocus(focused bool) {
	m.hasFocus = focused
}

// SetDimensions sets the dimensions of the log view.
func (m *LogView) SetDimensions(width, height int) {
	m.width = width
	m.height = height

	// Account for title line and panel borders
	viewportHeight := height - 3 // -1 for title, -2 for panel borders
	if viewportHeight < 3 {
		viewportHeight = 3
	}

	m.viewport.Width = width - 2 // Account for scrollbar
	m.viewport.Height = viewportHeight
	m.ready = true

	m.updateViewportContent()
}

// AddLog adds a new log entry.
func (m *LogView) AddLog(entry LogEntry) {
	m.logs = append(m.logs, entry)
	m.updateViewportContent()
}

// updateViewportContent refreshes the viewport text.
func (m *LogView) updateViewportContent() {
	if !m.ready {
		return
	}

	var logLines []string
	for _, entry := range m.logs {
		logLines = append(logLines, formatLogEntry(entry))
	}

	content := strings.Join(logLines, "\n")
	atBottom := m.viewport.AtBottom()

	m.viewport.SetContent(content)

	if atBottom || len(m.logs) == 1 {
		m.viewport.GotoBottom()
	}
}

// View renders the log view.
func (m LogView) View() string {
	if !m.ready {
		return ""
	}

	var logContent string
	if len(m.logs) == 0 {
		// Show empty state placeholder
		logContent = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true).
			Render("No logs yet...")
	} else {
		logContent = lipgloss.JoinHorizontal(lipgloss.Top,
			m.viewport.View(),
			m.renderScrollbar(),
		)
	}

	// Use focused or unfocused border style
	var panelStyle lipgloss.Style
	if m.hasFocus {
		panelStyle = m.styles.FocusedBorder
	} else {
		panelStyle = m.styles.DimBorder
	}

	return panelStyle.
		Width(m.width).
		MaxHeight(m.height).
		Render(
			m.styles.PanelTitle.Render("📋 System Logs") + "\n" +
				logContent,
		)
}

func (m LogView) renderScrollbar() string {
	totalLines := m.viewport.TotalLineCount()
	visibleLines := m.viewport.Height
	if totalLines <= visibleLines {
		return ""
	}

	scrollPercent := float64(m.viewport.YOffset) / float64(totalLines-visibleLines)
	if scrollPercent < 0 {
		scrollPercent = 0
	}
	if scrollPercent > 1 {
		scrollPercent = 1
	}

	barHeight := m.viewport.Height
	thumbHeight := int(float64(barHeight) * float64(visibleLines) / float64(totalLines))
	if thumbHeight < 1 {
		thumbHeight = 1
	}

	thumbPos := int(float64(barHeight-thumbHeight) * scrollPercent)

	var sb strings.Builder
	for i := 0; i < barHeight; i++ {
		if i >= thumbPos && i < thumbPos+thumbHeight {
			sb.WriteString(m.styles.ScrollbarThumb.Render("█"))
		} else {
			sb.WriteString(m.styles.Scrollbar.Render("│"))
		}
		if i < barHeight-1 {
			sb.WriteString("\n")
		}
	}

	return m.styles.Scrollbar.Render(sb.String())
}
