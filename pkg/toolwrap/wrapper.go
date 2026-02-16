package toolwrap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/logger"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const maxToolResultSize = 80000

// Wrapper wraps a tool to emit events when it's called.
// When WorkingMemory is set, it also caches results from file-read tools
// (read_file, list_file, read_multiple_files) to prevent redundant reads.
// When ApprovalStore is set, non-readonly tool calls require human approval
// before execution proceeds.
// maxConsecutiveRepeatCalls is the number of consecutive identical tool calls
// (same tool name + same arguments) that triggers loop detection. When the
// threshold is reached, the wrapper returns an error to the LLM instead of
// executing the tool again, breaking infinite loops.
const maxConsecutiveRepeatCalls = 3

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

	// callHistory tracks recent tool call fingerprints (toolName:args) to detect
	// infinite loops where the LLM repeatedly calls the same tool with identical
	// arguments. The slice acts as a bounded sliding window.
	callHistory []string
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

	// Loop detection: if the same tool+args has been called consecutively
	// maxConsecutiveRepeatCalls times, break the loop by returning an error
	// to the LLM so it can summarize existing results instead of retrying.
	fingerprint := toolName + ":" + string(jsonArgs)
	if w.isLooping(fingerprint) {
		logr.Warn("loop detected — same tool+args called consecutively",
			"tool", toolName, "times", maxConsecutiveRepeatCalls)
		return nil, fmt.Errorf(
			"loop detected: tool %s has been called with identical arguments %d times consecutively. "+
				"Stop calling this tool and summarize the results you already have",
			toolName, maxConsecutiveRepeatCalls)
	}
	w.recordCall(fingerprint)

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

	// Extract optional _justification from args (LLM explains why the tool is needed).
	// Strip it before forwarding to the actual tool so it doesn't leak into tool logic.
	justification, strippedArgs := extractJustification(jsonArgs)
	if justification != "" {
		jsonArgs = strippedArgs
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
				Type:          agui.EventToolApprovalRequest,
				ApprovalID:    approval.ID,
				ToolName:      toolName,
				Arguments:     string(jsonArgs),
				Justification: justification,
			}
		}

		logr.Info("waiting for human approval", "approval_id", approval.ID, "tool", toolName)
		resolved, waitErr := w.ApprovalStore.WaitForResolution(ctx, approval.ID)
		if waitErr != nil {
			return nil, fmt.Errorf("approval wait failed for tool %s: %w", toolName, waitErr)
		}
		if resolved.Status == hitl.StatusRejected {
			if resolved.Feedback != "" {
				w.storeFeedback(toolName, resolved.Feedback)
				logr.Info("tool call rejected by user with feedback", "tool", toolName, "feedback", resolved.Feedback)
				return nil, fmt.Errorf("tool call %s rejected by user: %s", toolName, resolved.Feedback)
			}
			logr.Info("tool call rejected by user", "tool", toolName)
			return nil, fmt.Errorf("tool call %s rejected by user", toolName)
		}
		if resolved.Feedback != "" {
			w.storeFeedback(toolName, resolved.Feedback)
			logr.Info("tool call approved with feedback — returning to LLM for re-planning", "tool", toolName, "feedback", resolved.Feedback)
			return nil, fmt.Errorf("tool call %s: user requested changes — %s — please adjust your approach and try again", toolName, resolved.Feedback)
		}
		logr.Info("tool call approved by user", "tool", toolName)
	}

	// Enrich context with EventChan/ThreadID/RunID so nested tools (e.g.
	// create_agent) can propagate HITL values to sub-agent tool wrappers.
	// The trpc-agent runner does not guarantee the AG-UI enriched context
	// flows through to tool Call methods, so we inject them explicitly here.
	//
	// Resolve effective values: prefer struct fields (set per-request via
	// WrapRequest), fall back to values already in the incoming context
	// (set by the AG-UI handler in server_expert.go). This ensures
	// propagation even when the parent expert doesn't pass threadID/runID
	// through WrapRequest.
	toolCtx := ctx
	if w.EventChan != nil && agui.EventChanFromContext(toolCtx) == nil {
		toolCtx = agui.WithEventChan(toolCtx, w.EventChan)
	}
	effectiveThreadID := w.ThreadID
	if effectiveThreadID == "" {
		effectiveThreadID = agui.ThreadIDFromContext(ctx)
	}
	if effectiveThreadID != "" && agui.ThreadIDFromContext(toolCtx) == "" {
		toolCtx = agui.WithThreadID(toolCtx, effectiveThreadID)
	}
	effectiveRunID := w.RunID
	if effectiveRunID == "" {
		effectiveRunID = agui.RunIDFromContext(ctx)
	}
	if effectiveRunID != "" && agui.RunIDFromContext(toolCtx) == "" {
		toolCtx = agui.WithRunID(toolCtx, effectiveRunID)
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
		logr.Error("tool call failed", "tool", toolName, "args", string(jsonArgs), "error", err)
	} else {
		logr.Debug("tool call completed", "tool", toolName, "response_length", len(responseStr), "truncated", truncated)
	}

	// Audit: log tool call
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	if w.Auditor != nil {
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

// isLooping returns true when the most recent (maxConsecutiveRepeatCalls-1)
// entries in callHistory all match the given fingerprint. Combined with the
// current call this means the tool would have been invoked
// maxConsecutiveRepeatCalls times consecutively with the same arguments.
func (w *Wrapper) isLooping(fingerprint string) bool {
	n := len(w.callHistory)
	needed := maxConsecutiveRepeatCalls - 1
	if n < needed {
		return false
	}
	for i := n - needed; i < n; i++ {
		if w.callHistory[i] != fingerprint {
			return false
		}
	}
	return true
}

// recordCall appends a fingerprint to callHistory and trims the slice to the
// most recent 10 entries to bound memory usage.
func (w *Wrapper) recordCall(fingerprint string) {
	w.callHistory = append(w.callHistory, fingerprint)
	const maxHistory = 10
	if len(w.callHistory) > maxHistory {
		w.callHistory = w.callHistory[len(w.callHistory)-maxHistory:]
	}
}

// extractJustification pulls the optional "_justification" key from a JSON
// tool-call argument blob. It returns the justification string and the
// arguments with the key removed. If the key is absent or the blob is not
// valid JSON, the original args are returned unchanged with an empty
// justification.
func extractJustification(args []byte) (string, []byte) {
	justification := gjson.GetBytes(args, "_justification")
	if !justification.Exists() {
		return "", args
	}

	stripped, err := sjson.DeleteBytes(args, "_justification")
	if err != nil {
		return "", args
	}
	return justification.String(), stripped
}

// storeFeedback persists user HITL feedback into WorkingMemory so that
// current and future sub-agents can learn from the correction. Feedback
// is stored under "hitl:feedback:<toolName>" keys and is automatically
// injected into sub-agent prompts via the WorkingMemory snapshot in
// create_agent.go. Multiple feedback entries for the same tool are
// concatenated with newlines.
func (w *Wrapper) storeFeedback(toolName, feedback string) {
	if w.WorkingMemory == nil || feedback == "" {
		return
	}
	key := fmt.Sprintf("hitl:feedback:%s", toolName)
	if existing, ok := w.WorkingMemory.Recall(key); ok && existing != "" {
		w.WorkingMemory.Store(key, existing+"\n"+feedback)
	} else {
		w.WorkingMemory.Store(key, feedback)
	}
}
