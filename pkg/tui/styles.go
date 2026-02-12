package tui

import "github.com/charmbracelet/lipgloss"

// Styles contains all the styling for the TUI components.
// This centralizes all visual styling using Lip Gloss.
type Styles struct {
	Header          lipgloss.Style
	Content         lipgloss.Style
	Footer          lipgloss.Style
	Error           lipgloss.Style
	Success         lipgloss.Style
	Thinking        lipgloss.Style
	StageComplete   lipgloss.Style
	StageCurrent    lipgloss.Style
	StagePending    lipgloss.Style
	ToolName        lipgloss.Style
	ToolPending     lipgloss.Style
	ToolComplete    lipgloss.Style
	ToolError       lipgloss.Style
	ToolResponse    lipgloss.Style
	Panel           lipgloss.Style
	PanelTitle      lipgloss.Style
	FocusedBorder   lipgloss.Style
	DimBorder       lipgloss.Style
	FocusedTitle    lipgloss.Style
	DimTitle        lipgloss.Style
	LogDebug        lipgloss.Style
	LogInfo         lipgloss.Style
	LogWarn         lipgloss.Style
	LogError        lipgloss.Style
	Scrollbar       lipgloss.Style
	ScrollbarThumb  lipgloss.Style
	UserBubble      lipgloss.Style
	AiBubble        lipgloss.Style
	ToolCard        lipgloss.Style
	ToolCardIcon    lipgloss.Style
	SystemBubble    lipgloss.Style
	ReasoningBubble lipgloss.Style
	DiffAdd         lipgloss.Style
	DiffRemove      lipgloss.Style
	ErrorBubble     lipgloss.Style
}

// DefaultStyles returns the default styling for the TUI.
// This uses a vibrant, modern color palette optimized for dark terminals.
func DefaultStyles() Styles {
	// Color palette - vibrant and modern
	var (
		primaryColor = lipgloss.Color("#7C3AED") // Vibrant purple
		successColor = lipgloss.Color("#10B981") // Emerald green
		errorColor   = lipgloss.Color("#EF4444") // Bright red
		warningColor = lipgloss.Color("#F59E0B") // Amber
		accentColor  = lipgloss.Color("#06B6D4") // Cyan
		mutedColor   = lipgloss.Color("#6B7280") // Gray
		textColor    = lipgloss.Color("#F9FAFB") // Off-white
	)

	return Styles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1, 10).
			MarginBottom(1),

		Content: lipgloss.NewStyle().
			Foreground(textColor).
			Padding(0, 2).
			MarginBottom(1),

		Footer: lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true).
			MarginTop(1),

		Error: lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(errorColor).
			Padding(1, 2).
			MarginTop(1),

		Success: lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(successColor).
			Padding(1, 2).
			MarginTop(1),

		Thinking: lipgloss.NewStyle().
			Foreground(accentColor).
			Italic(true).
			MarginBottom(1),

		StageComplete: lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true).
			MarginRight(2),

		StageCurrent: lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true).
			MarginRight(2),

		StagePending: lipgloss.NewStyle().
			Foreground(mutedColor).
			MarginRight(2),

		ToolName: lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true),

		ToolPending: lipgloss.NewStyle().
			Foreground(warningColor).
			Italic(true),

		ToolComplete: lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true),

		ToolError: lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true),

		ToolResponse: lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true),

		Panel: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Padding(0, 1).
			MarginTop(1),

		PanelTitle: lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true),

		FocusedBorder: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Padding(0, 1),

		DimBorder: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(mutedColor).
			Padding(0, 1),

		FocusedTitle: lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true),

		DimTitle: lipgloss.NewStyle().
			Foreground(mutedColor).
			Bold(false),

		LogDebug: lipgloss.NewStyle().
			Foreground(mutedColor),

		LogInfo: lipgloss.NewStyle().
			Foreground(accentColor),

		LogWarn: lipgloss.NewStyle().
			Foreground(warningColor),

		LogError: lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true),

		Scrollbar: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(mutedColor),

		ScrollbarThumb: lipgloss.NewStyle().
			Foreground(primaryColor),

		UserBubble: lipgloss.NewStyle().
			Foreground(textColor).
			Background(primaryColor).
			Padding(1, 2).
			MarginLeft(4).
			MarginBottom(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor),

		AiBubble: lipgloss.NewStyle().
			Foreground(textColor).
			Background(mutedColor).
			Padding(1, 2).
			MarginRight(4).
			MarginBottom(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mutedColor),

		ToolCard: lipgloss.NewStyle().
			Foreground(textColor).
			Padding(0, 2).
			MarginBottom(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor),

		ToolCardIcon: lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true),

		SystemBubble: lipgloss.NewStyle().
			Foreground(accentColor).
			Padding(1, 2).
			MarginBottom(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor),

		ReasoningBubble: lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true).
			Padding(1, 2).
			MarginRight(4).
			MarginBottom(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mutedColor),

		DiffAdd: lipgloss.NewStyle().
			Foreground(successColor),

		DiffRemove: lipgloss.NewStyle().
			Foreground(errorColor),

		ErrorBubble: lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true).
			Padding(1, 2).
			MarginBottom(1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(errorColor),
	}
}
