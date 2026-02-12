package tui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// toolCallState tracks the state of an in-progress tool call.
// This enables updating tool card status from ⟳ (running) to ✓ (done) or ✗ (error)
// when the tool response arrives, providing real-time feedback in the chat.
type toolCallState struct {
	ToolName   string
	Arguments  string
	ToolCallID string
	Status     string // "running", "done", "error"
}

// summarizeToolArgs extracts a human-readable summary from tool call arguments.
// Different tools have different argument formats, so this function knows how to
// extract the most relevant field for each known tool name.
//
// For example: save_file args {"file_name":"main.tf","content":"..."} → "main.tf"
func summarizeToolArgs(toolName, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}

	// Extract the most relevant argument based on tool name
	switch {
	case strings.Contains(toolName, "read"), strings.Contains(toolName, "list"):
		if path, ok := args["file_name"].(string); ok {
			return path
		}
		if path, ok := args["path"].(string); ok {
			return path
		}
		if dir, ok := args["directory"].(string); ok {
			return dir
		}
	case strings.Contains(toolName, "save"), strings.Contains(toolName, "write"):
		if path, ok := args["file_name"].(string); ok {
			return path
		}
		if path, ok := args["path"].(string); ok {
			return path
		}
	case strings.Contains(toolName, "search"), strings.Contains(toolName, "grep"):
		if pattern, ok := args["pattern"].(string); ok {
			return fmt.Sprintf(`"%s"`, pattern)
		}
		if query, ok := args["query"].(string); ok {
			return fmt.Sprintf(`"%s"`, query)
		}
	case strings.Contains(toolName, "replace"):
		if path, ok := args["file_name"].(string); ok {
			return path
		}
	}

	// Fallback: show first string argument value
	for _, v := range args {
		if s, ok := v.(string); ok && len(s) < 60 {
			return s
		}
	}
	return ""
}

// summarizeToolResult creates a short summary of a tool response.
// Long outputs are truncated to keep tool cards compact in the chat view.
func summarizeToolResult(toolName, response string) string {
	if response == "" {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(response), "\n")

	switch {
	case strings.Contains(toolName, "read"):
		if len(lines) == 1 {
			return "1 line read"
		}
		return fmt.Sprintf("%d lines read", len(lines))
	case strings.Contains(toolName, "save"), strings.Contains(toolName, "write"):
		if len(lines) == 1 {
			return "1 line written"
		}
		return fmt.Sprintf("%d lines written", len(lines))
	case strings.Contains(toolName, "list"):
		if len(lines) == 1 {
			return "1 item"
		}
		return fmt.Sprintf("%d items", len(lines))
	case strings.Contains(toolName, "search"), strings.Contains(toolName, "grep"):
		if len(lines) == 1 {
			return "1 match"
		}
		return fmt.Sprintf("%d matches", len(lines))
	case strings.Contains(toolName, "replace"):
		return "content replaced"
	}

	// Generic: first 40 runes (accounting for ellipsis suffix)
	if runes := []rune(response); len(runes) > 40 {
		return string(runes[:39]) + "…"
	}
	return response
}

// maxDiffLines is the maximum number of diff lines shown in a tool card preview.
const maxDiffLines = 6

// extractDiffPreview extracts a compact diff preview from tool call arguments.
// For save_file: shows the first few lines of content being written (all additions).
// For replace_content: shows old lines as removals (-) and new lines as additions (+).
// Returns empty string if no diff can be extracted or the tool isn't a write operation.
func extractDiffPreview(toolName, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}

	switch {
	case strings.Contains(toolName, "save"), strings.Contains(toolName, "write"):
		content, ok := args["content"].(string)
		if !ok || content == "" {
			return ""
		}
		return formatAdditions(content)

	case strings.Contains(toolName, "replace"):
		oldContent, _ := args["old_content"].(string)
		newContent, _ := args["new_content"].(string)
		if oldContent == "" && newContent == "" {
			return ""
		}
		return formatReplaceDiff(oldContent, newContent)
	}

	return ""
}

// formatAdditions formats content as a diff showing all lines as additions.
func formatAdditions(content string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	var sb strings.Builder

	shown := min(len(lines), maxDiffLines)
	for i := 0; i < shown; i++ {
		line := lines[i]
		if runes := []rune(line); len(runes) > 60 {
			line = string(runes[:57]) + "..."
		}
		sb.WriteString(fmt.Sprintf("+ %s\n", line))
	}
	if len(lines) > maxDiffLines {
		sb.WriteString(fmt.Sprintf("  ... +%d more lines\n", len(lines)-maxDiffLines))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatReplaceDiff formats a replace operation as a unified-style diff preview.
func formatReplaceDiff(oldContent, newContent string) string {
	var sb strings.Builder

	if oldContent != "" {
		oldLines := strings.Split(strings.TrimRight(oldContent, "\n"), "\n")
		shown := min(len(oldLines), maxDiffLines/2)
		for i := 0; i < shown; i++ {
			line := oldLines[i]
			if runes := []rune(line); len(runes) > 60 {
				line = string(runes[:57]) + "..."
			}
			sb.WriteString(fmt.Sprintf("- %s\n", line))
		}
		if len(oldLines) > maxDiffLines/2 {
			sb.WriteString(fmt.Sprintf("  ... -%d more lines\n", len(oldLines)-maxDiffLines/2))
		}
	}

	if newContent != "" {
		newLines := strings.Split(strings.TrimRight(newContent, "\n"), "\n")
		shown := min(len(newLines), maxDiffLines/2)
		for i := 0; i < shown; i++ {
			line := newLines[i]
			if runes := []rune(line); len(runes) > 60 {
				line = string(runes[:57]) + "..."
			}
			sb.WriteString(fmt.Sprintf("+ %s\n", line))
		}
		if len(newLines) > maxDiffLines/2 {
			sb.WriteString(fmt.Sprintf("  ... +%d more lines\n", len(newLines)-maxDiffLines/2))
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}
