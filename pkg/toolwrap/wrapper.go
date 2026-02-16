package toolwrap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/hitl"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/go-lib/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Wrapper wraps a tool to emit events when it's called.
// When WorkingMemory is set, it also caches results from file-read tools
// (read_file, list_file, read_multiple_files) to prevent redundant reads.
// When ApprovalStore is set, non-readonly tool calls require human approval
// before execution proceeds.
type Wrapper struct {
	tool.Tool
	EventChan     chan<- interface{}
	WorkingMemory *rtmemory.WorkingMemory
	Auditor       audit.Auditor

	// ApprovalStore enables human-in-the-loop approval for non-readonly tools.
	// When nil, all tools execute immediately (HITL disabled).
	ApprovalStore hitl.ApprovalStore
	// ThreadID and RunID identify the current AG-UI session for the approval request.
	ThreadID string
	RunID    string
}

func (w *Wrapper) Call(ctx context.Context, jsonArgs []byte) (output any, err error) {
	logr := logger.GetLogger(ctx).With("fn", "Wrapper.Call", "tool", w.Tool.Declaration().Name)

	// Recover from panics (e.g. OTel recordingSpan.End "send on closed channel")
	// to prevent the entire server from crashing.
	defer func() {
		if r := recover(); r != nil {
			logr.Error("recovered panic in tool call", "panic", r, "tool", w.Tool.Declaration().Name)
			output = nil
			err = fmt.Errorf("internal error in tool %s: %v", w.Tool.Declaration().Name, r)
		}
	}()

	// Log tool invocation
	logr.Debug("tool call started", "args", string(jsonArgs))
	defer func(startTime time.Time) {
		logr.Debug("tool call completed", "tool", w.Tool.Declaration().Name, "duration", time.Since(startTime).String())
	}(time.Now())

	toolName := w.Tool.Declaration().Name

	// Cache check: return cached result for file-read tools if available.
	if cached, ok := w.checkFileCache(logr, toolName, jsonArgs); ok {
		// Emit event even for cache hits so the TUI stays informed
		if w.EventChan != nil {
			w.EventChan <- agui.AgentToolResponseMsg{
				Type:     agui.EventToolCallResult,
				ToolName: toolName,
				Response: fmt.Sprintf("%v", cached),
			}
		}
		return cached, nil
	}

	// HITL approval gate: non-readonly tools require human approval before execution.
	if w.ApprovalStore != nil && !w.ApprovalStore.IsAllowed(toolName) {
		// Prefer struct-level ThreadID/RunID, fall back to context values
		// injected by the AG-UI handler.
		threadID := w.ThreadID
		if threadID == "" {
			threadID = agui.ThreadIDFromContext(ctx)
		}
		runID := w.RunID
		if runID == "" {
			runID = agui.RunIDFromContext(ctx)
		}

		logr.Info("HITL approval gate entered",
			"threadID", threadID,
			"runID", runID,
			"hasStructEventChan", w.EventChan != nil,
			"hasCtxEventChan", agui.EventChanFromContext(ctx) != nil,
		)

		approval, createErr := w.ApprovalStore.Create(ctx, hitl.CreateRequest{
			ThreadID: threadID,
			RunID:    runID,
			ToolName: toolName,
			Args:     string(jsonArgs),
		})
		if createErr != nil {
			return nil, fmt.Errorf("failed to create approval request for tool %s: %w", toolName, createErr)
		}

		// Emit approval request event to the UI.
		// Fall back to context EventChan for sub-agent tool calls.
		evChan := w.EventChan
		if evChan == nil {
			evChan = agui.EventChanFromContext(ctx)
		}
		if evChan != nil {
			evChan <- agui.ToolApprovalRequestMsg{
				Type:       agui.EventToolApprovalRequest,
				ApprovalID: approval.ID,
				ToolName:   toolName,
				Arguments:  string(jsonArgs),
			}
		}

		logr.Info("waiting for human approval", "approval_id", approval.ID, "tool", toolName)
		resolved, waitErr := w.ApprovalStore.WaitForResolution(ctx, approval.ID)
		if waitErr != nil {
			return nil, fmt.Errorf("approval wait failed for tool %s: %w", toolName, waitErr)
		}
		if resolved.Status == hitl.StatusRejected {
			logr.Info("tool call rejected by user", "tool", toolName)
			return nil, fmt.Errorf("tool call %s rejected by user", toolName)
		}
		logr.Info("tool call approved by user", "tool", toolName)
	}

	// Enrich context with EventChan/ThreadID/RunID so nested tools (e.g.
	// create_agent) can propagate HITL values to sub-agent tool wrappers.
	// The trpc-agent runner does not guarantee the AG-UI enriched context
	// flows through to tool Call methods, so we inject them explicitly here.
	toolCtx := ctx
	if w.EventChan != nil && agui.EventChanFromContext(toolCtx) == nil {
		toolCtx = agui.WithEventChan(toolCtx, w.EventChan)
	}
	if w.ThreadID != "" && agui.ThreadIDFromContext(toolCtx) == "" {
		toolCtx = agui.WithThreadID(toolCtx, w.ThreadID)
	}
	if w.RunID != "" && agui.RunIDFromContext(toolCtx) == "" {
		toolCtx = agui.WithRunID(toolCtx, w.RunID)
	}

	if ct, ok := w.Tool.(tool.CallableTool); ok {
		output, err = ct.Call(toolCtx, jsonArgs)
	} else {
		return nil, fmt.Errorf("tool is not callable")
	}

	// Cache store: persist successful file-read results before truncation.
	if err == nil {
		w.storeFileCache(logr, toolName, jsonArgs, output)
	}

	// Token optimization: Truncate large tool responses to prevent context explosion
	// Large outputs (like generated Terraform files) can bloat context in subsequent LLM calls
	const maxToolResultSize = 2000
	responseStr := fmt.Sprintf("%v", output)
	truncated := false
	if len(responseStr) > maxToolResultSize {
		end := maxToolResultSize
		// To avoid corrupting a multi-byte character, find the last rune boundary.
		for end > 0 && (responseStr[end]&0xC0) == 0x80 { // is a continuation byte
			end--
		}
		responseStr = responseStr[:end] + "\n... [truncated - full output saved to file]"
		truncated = true
	}

	// Log tool result
	if err != nil {
		logr.Error("tool call failed", "error", err)
	} else {
		logr.Debug("tool call completed", "response_length", len(responseStr), "truncated", truncated)
	}

	// Audit: log tool call
	if w.Auditor != nil {
		errStr := ""
		if err != nil {
			errStr = err.Error()
		}
		w.Auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventToolCall,
			Actor:     "expert",
			Action:    toolName,
			Metadata: map[string]interface{}{
				"args":            TruncateForAudit(string(jsonArgs), 200),
				"response_length": len(responseStr),
				"truncated":       truncated,
				"error":           errStr,
			},
		})
	}

	// Emit tool response event
	if w.EventChan != nil {
		w.EventChan <- agui.AgentToolResponseMsg{
			Type:     agui.EventToolCallResult,
			ToolName: toolName,
			Response: responseStr,
			Error:    err,
		}
	}

	return output, err
}

// cacheableFileTools is the set of tool names whose results are cached in WorkingMemory.
var cacheableFileTools = map[string]bool{
	"read_file":           true,
	"list_file":           true,
	"read_multiple_files": true,
}

// fileCacheKey builds a WorkingMemory key from the tool name and its arguments.
// For read_file it uses "tool:read_file:<file_name>", for list_file "tool:list_file:<path>".
func fileCacheKey(toolName string, jsonArgs []byte) (string, bool) {
	if !cacheableFileTools[toolName] {
		return "", false
	}

	var args map[string]any
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return "", false
	}

	// Build a unique key from the tool name + primary identifier
	switch toolName {
	case "read_file":
		if fn, ok := args["file_name"].(string); ok {
			return fmt.Sprintf("tool:read_file:%s", fn), true
		}
	case "list_file":
		path, _ := args["path"].(string)
		return fmt.Sprintf("tool:list_file:%s", path), true
	case "read_multiple_files":
		// Use the full args JSON as key since patterns can vary
		return fmt.Sprintf("tool:read_multiple_files:%s", string(jsonArgs)), true
	}

	return "", false
}

// checkFileCache returns a cached tool result if one exists in WorkingMemory.
func (w *Wrapper) checkFileCache(logr interface {
	Debug(msg string, keysAndValues ...any)
}, toolName string, jsonArgs []byte) (any, bool) {
	if w.WorkingMemory == nil {
		return nil, false
	}

	key, ok := fileCacheKey(toolName, jsonArgs)
	if !ok {
		return nil, false
	}

	if cached, found := w.WorkingMemory.Recall(key); found {
		logr.Debug("file cache hit — skipping redundant tool call", "key", key)
		return cached, true
	}

	return nil, false
}

// storeFileCache persists a successful file-read tool result into WorkingMemory.
func (w *Wrapper) storeFileCache(logr interface {
	Debug(msg string, keysAndValues ...any)
}, toolName string, jsonArgs []byte, output any) {
	if w.WorkingMemory == nil {
		return
	}

	key, ok := fileCacheKey(toolName, jsonArgs)
	if !ok {
		return
	}

	// Store the full (un-truncated) result in working memory.
	w.WorkingMemory.Store(key, fmt.Sprintf("%v", output))
	logr.Debug("file result cached in working memory", "key", key)
}

// TruncateForAudit truncates a string to maxLen runes for audit log metadata.
// Appends "…" when truncated to signal the value was trimmed.
func TruncateForAudit(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}
