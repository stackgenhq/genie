package toolwrap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/ttlcache"
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

// maxConsecutiveToolFailures is the number of consecutive failures (any args)
// for the same tool that triggers a hard block. This prevents the LLM from
// endlessly retrying a tool with slightly reworded arguments when the
// underlying service (e.g. DuckDuckGo) is rate-limited or down.
const maxConsecutiveToolFailures = 3

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

	// callMu protects callHistory and semanticCache from concurrent access.
	callMu sync.Mutex

	// callHistory tracks recent tool call fingerprints (toolName:args) to detect
	// infinite loops where the LLM repeatedly calls the same tool with identical
	// arguments. The slice acts as a bounded sliding window.
	callHistory []string

	// approvalCache remembers tool+args combinations that have been approved
	// during the current session. Keyed by SHA-256 of (threadID + toolName + args).
	// This prevents re-prompting the user for the same tool call within a session.
	// Bounded to maxApprovalCacheSize entries with FIFO eviction.
	approvalMu    sync.Mutex
	approvalCache map[string]struct{}
	approvalOrder []string

	// semanticCache prevents re-executing idempotent tools when the LLM
	// varies non-key arguments (like action text) across iterations.
	// Unlike the approval cache (which only skips re-prompting), this
	// skips the entire tool execution including HITL.
	// Entries have a TTL so legitimate re-executions (minutes later) are not blocked.
	semanticCache *ttlcache.TTLMap[any]

	// failureCount tracks consecutive failures per tool name. When a tool
	// fails maxConsecutiveToolFailures times in a row (regardless of args),
	// further calls to that tool are blocked. A successful call resets the counter.
	failureCount map[string]int
}

// semanticCacheTTL is how long a semantic cache entry stays valid.
// 120s is long enough to block the LLM's rapid-fire retry loop (calls happen
// seconds apart) but short enough so that legitimate re-executions minutes
// later go through normally.
const semanticCacheTTL = 120 * time.Second

// maxSemanticCacheSize limits the number of entries to prevent unbounded growth.
const maxSemanticCacheSize = 128

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

	logr.Debug("tool call started", "args", string(jsonArgs))
	defer func(t time.Time) {
		logr.Debug("tool call completed", "tool", w.Tool.Declaration().Name, "duration", time.Since(t).String())
	}(time.Now())

	toolName := w.Tool.Declaration().Name

	// --- Pre-execution checks (each may short-circuit) ---

	if err := w.checkLoopDetection(toolName, jsonArgs); err != nil {
		return nil, err
	}

	if err := w.checkToolFailureLimit(toolName); err != nil {
		return nil, err
	}

	if cached, ok := w.checkSemanticCache(logr, toolName, jsonArgs); ok {
		return cached, nil
	}

	if cached, ok := w.checkFileCache(logr, toolName, jsonArgs); ok {
		w.emitToolResponse(toolName, fmt.Sprintf("%v", cached), nil)
		return cached, nil
	}

	// Strip _justification before forwarding to the tool.
	justification, strippedArgs := extractJustification(jsonArgs)
	if justification != "" {
		jsonArgs = strippedArgs
	}

	// HITL approval gate (skipped when ApprovalStore is nil).
	if err := w.requireApproval(ctx, logr, toolName, jsonArgs, justification); err != nil {
		return nil, err
	}

	// --- Execute ---

	toolCtx := w.enrichContext(ctx)
	ct, ok := w.Tool.(tool.CallableTool)
	if !ok {
		return nil, fmt.Errorf("tool is not callable")
	}
	output, err = ct.Call(toolCtx, jsonArgs)

	// --- Post-execution ---

	w.postProcess(ctx, logr, toolName, jsonArgs, output, err)
	w.recordToolOutcome(toolName, err)
	return output, err
}

// checkLoopDetection detects consecutive identical tool calls. Returns an
// error when the loop threshold is reached, nil otherwise.
func (w *Wrapper) checkLoopDetection(toolName string, jsonArgs []byte) error {
	fingerprint := toolName + ":" + string(jsonArgs)
	w.callMu.Lock()
	looping := w.isLooping(fingerprint)
	if !looping {
		w.recordCall(fingerprint)
	}
	w.callMu.Unlock()
	if looping {
		return fmt.Errorf(
			"loop detected: tool %s has been called with identical arguments %d times consecutively. "+
				"Stop calling this tool and summarize the results you already have",
			toolName, maxConsecutiveRepeatCalls)
	}
	return nil
}

// checkToolFailureLimit blocks a tool after maxConsecutiveToolFailures
// consecutive failures, regardless of arguments. This catches the case where
// the LLM varies arguments slightly (e.g. rephrasing a search query) but the
// underlying service is rate-limited or down.
func (w *Wrapper) checkToolFailureLimit(toolName string) error {
	w.callMu.Lock()
	count := w.failureCount[toolName]
	w.callMu.Unlock()
	if count >= maxConsecutiveToolFailures {
		return fmt.Errorf(
			"tool %s has failed %d times consecutively. The service may be rate-limited or down. "+
				"Stop calling this tool and report the failure to the user",
			toolName, count)
	}
	return nil
}

// recordToolOutcome updates the per-tool failure counter after a call.
// Successful calls reset the counter; failures increment it.
func (w *Wrapper) recordToolOutcome(toolName string, err error) {
	w.callMu.Lock()
	defer w.callMu.Unlock()
	if w.failureCount == nil {
		w.failureCount = make(map[string]int)
	}
	if err != nil {
		w.failureCount[toolName]++
	} else {
		delete(w.failureCount, toolName)
	}
}

// checkSemanticCache checks whether an idempotent tool call has a cached
// result (e.g. create_recurring_task with the same name). Returns the
// cached value and true on hit, emitting the appropriate events.
func (w *Wrapper) checkSemanticCache(logr interface {
	Debug(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}, toolName string, jsonArgs []byte) (any, bool) {
	semKey, ok := semanticKey(toolName, jsonArgs)
	if !ok {
		return nil, false
	}

	w.callMu.Lock()
	if w.semanticCache == nil {
		w.semanticCache = ttlcache.NewTTLMap[any](maxSemanticCacheSize, semanticCacheTTL)
	}
	cached, hit := w.semanticCache.Get(semKey)
	w.callMu.Unlock()

	if !hit {
		return nil, false
	}

	logr.Debug("semantic cache hit — returning cached tool result", "key", semKey)
	responseStr, truncated := truncateResponse(fmt.Sprintf("%v", cached))
	w.logResult(logr, toolName, jsonArgs, responseStr, truncated, nil)
	w.emitToolResponse(toolName, responseStr, nil)
	return cached, true
}

// postProcess handles all post-execution bookkeeping: caching results,
// invalidating stale file reads, truncating output, logging, auditing,
// and emitting the response event.
func (w *Wrapper) postProcess(ctx context.Context, logr interface {
	Debug(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}, toolName string, jsonArgs []byte, output any, err error) {
	if err == nil {
		w.storeFileCache(logr, toolName, jsonArgs, output)
		w.storeSemanticCache(logr, toolName, jsonArgs, output)
	}
	w.invalidateFileCacheOnWrite(toolName, jsonArgs)

	responseStr, truncated := truncateResponse(fmt.Sprintf("%v", output))
	w.logResult(logr, toolName, jsonArgs, responseStr, truncated, err)
	w.auditToolCall(ctx, toolName, jsonArgs, responseStr, truncated, err)
	w.emitToolResponse(toolName, responseStr, err)
}

// storeSemanticCache persists a successful idempotent tool result under its
// semantic key so that duplicate calls within the TTL hit the cache.
func (w *Wrapper) storeSemanticCache(logr interface {
	Debug(msg string, keysAndValues ...any)
}, toolName string, jsonArgs []byte, output any) {
	semKey, ok := semanticKey(toolName, jsonArgs)
	if !ok {
		return
	}
	w.callMu.Lock()
	if w.semanticCache == nil {
		w.semanticCache = ttlcache.NewTTLMap[any](maxSemanticCacheSize, semanticCacheTTL)
	}
	w.semanticCache.Set(semKey, output)
	w.callMu.Unlock()
	logr.Debug("semantic cache stored", "key", semKey, "ttl", semanticCacheTTL)
}

// requireApproval blocks on human approval for non-readonly tools. It checks
// the session-scoped approval cache first to avoid re-prompting for the same
// tool+args combination within a session. Returns nil when the tool may proceed.
func (w *Wrapper) requireApproval(ctx context.Context, logr interface {
	Debug(msg string, keysAndValues ...any)
	Info(msg string, keysAndValues ...any)
}, toolName string, jsonArgs []byte, justification string) error {
	if w.ApprovalStore == nil || w.ApprovalStore.IsAllowed(toolName) {
		return nil
	}

	threadID := w.effectiveThreadID(ctx)
	runID := w.effectiveRunID(ctx)

	// Session-scoped approval cache: skip re-prompting if the same
	// tool+args was already approved in this session.
	approvalKey := approvalFingerprint(threadID, toolName, string(jsonArgs))
	w.approvalMu.Lock()
	if w.approvalCache == nil {
		w.approvalCache = make(map[string]struct{})
	}
	_, cached := w.approvalCache[approvalKey]
	w.approvalMu.Unlock()
	if cached {
		logr.Debug("HITL cache hit — auto-approved (same session + tool + args)",
			"tool", toolName)
		return nil
	}

	logr.Info("HITL approval gate entered",
		"threadID", threadID, "runID", runID,
		"hasStructEventChan", w.EventChan != nil,
		"hasCtxEventChan", agui.EventChanFromContext(ctx) != nil,
	)

	approval, err := w.ApprovalStore.Create(ctx, hitl.CreateRequest{
		ThreadID:      threadID,
		RunID:         runID,
		ToolName:      toolName,
		Args:          string(jsonArgs),
		SenderContext: messenger.SenderContextFrom(ctx),
		Question:      OriginalQuestionFrom(ctx),
	})
	if err != nil {
		return fmt.Errorf("failed to create approval request for tool %s: %w", toolName, err)
	}

	// Emit approval request event to the UI.
	w.emitApprovalRequest(ctx, approval.ID, toolName, string(jsonArgs), justification)

	logr.Info("waiting for human approval", "approval_id", approval.ID, "tool", toolName)
	resolved, err := w.ApprovalStore.WaitForResolution(ctx, approval.ID)
	if err != nil {
		return fmt.Errorf("approval wait failed for tool %s: %w", toolName, err)
	}

	switch {
	case resolved.Status == hitl.StatusRejected && resolved.Feedback != "":
		w.storeFeedback(toolName, resolved.Feedback)
		logr.Info("tool call rejected with feedback", "tool", toolName, "feedback", resolved.Feedback)
		return fmt.Errorf("tool call %s rejected by user: %s", toolName, resolved.Feedback)

	case resolved.Status == hitl.StatusRejected:
		logr.Info("tool call rejected by user", "tool", toolName)
		return fmt.Errorf("tool call %s rejected by user", toolName)

	case resolved.Feedback != "":
		w.storeFeedback(toolName, resolved.Feedback)
		logr.Info("tool call approved with feedback — returning to LLM for re-planning",
			"tool", toolName, "feedback", resolved.Feedback)
		return fmt.Errorf("tool call %s: user requested changes — %s — please adjust your approach and try again",
			toolName, resolved.Feedback)
	}

	logr.Info("tool call approved by user", "tool", toolName)
	w.storeApproval(approvalKey)
	return nil
}

// emitApprovalRequest sends a TOOL_APPROVAL_REQUEST event to the UI. Falls
// back to context EventChan for sub-agent tool calls. Uses a non-blocking
// send to prevent deadlock if the channel consumer is slow.
func (w *Wrapper) emitApprovalRequest(ctx context.Context, approvalID, toolName, args, justification string) {
	evChan := w.EventChan
	if evChan == nil {
		evChan = agui.EventChanFromContext(ctx)
	}
	if evChan != nil {
		select {
		case evChan <- agui.ToolApprovalRequestMsg{
			Type:          agui.EventToolApprovalRequest,
			ApprovalID:    approvalID,
			ToolName:      toolName,
			Arguments:     args,
			Justification: justification,
		}:
		case <-ctx.Done():
		}
	}
}

// enrichContext injects EventChan/ThreadID/RunID into the context so nested
// tools (e.g. create_agent) can propagate HITL values to sub-agent wrappers.
func (w *Wrapper) enrichContext(ctx context.Context) context.Context {
	toolCtx := ctx
	if w.EventChan != nil && agui.EventChanFromContext(toolCtx) == nil {
		toolCtx = agui.WithEventChan(toolCtx, w.EventChan)
	}
	if tid := w.effectiveThreadID(ctx); tid != "" && agui.ThreadIDFromContext(toolCtx) == "" {
		toolCtx = agui.WithThreadID(toolCtx, tid)
	}
	if rid := w.effectiveRunID(ctx); rid != "" && agui.RunIDFromContext(toolCtx) == "" {
		toolCtx = agui.WithRunID(toolCtx, rid)
	}
	return toolCtx
}

// effectiveThreadID returns the struct-level ThreadID, falling back to the
// context value set by the AG-UI handler.
func (w *Wrapper) effectiveThreadID(ctx context.Context) string {
	if w.ThreadID != "" {
		return w.ThreadID
	}
	return agui.ThreadIDFromContext(ctx)
}

// effectiveRunID returns the struct-level RunID, falling back to the context
// value set by the AG-UI handler.
func (w *Wrapper) effectiveRunID(ctx context.Context) string {
	if w.RunID != "" {
		return w.RunID
	}
	return agui.RunIDFromContext(ctx)
}

// truncateResponse caps a response string at maxToolResultSize, respecting
// multi-byte character boundaries using utf8.RuneStart. Returns the
// (possibly truncated) string and whether truncation occurred.
func truncateResponse(s string) (string, bool) {
	if len(s) <= maxToolResultSize {
		return s, false
	}
	end := maxToolResultSize
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "\n... [truncated - full output saved to file]", true
}

// logResult logs the outcome of a tool call.
func (w *Wrapper) logResult(logr interface {
	Debug(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}, toolName string, jsonArgs []byte, responseStr string, truncated bool, err error) {
	if err != nil {
		logr.Error("tool call failed", "tool", toolName, "args", string(jsonArgs), "error", err)
	} else {
		logr.Debug("tool call completed", "tool", toolName, "response_length", len(responseStr), "truncated", truncated)
	}
}

// auditToolCall logs the tool call to the audit trail.
func (w *Wrapper) auditToolCall(ctx context.Context, toolName string, jsonArgs []byte, responseStr string, truncated bool, err error) {
	if w.Auditor == nil {
		return
	}
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	w.Auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventToolCall,
		Actor:     "expert",
		Action:    toolName,
		Metadata: map[string]interface{}{
			"args":            redactSensitiveArgs(jsonArgs),
			"response_length": len(responseStr),
			"truncated":       truncated,
			"error":           errStr,
		},
	})
}

// emitToolResponse sends the tool call result to the event channel for the TUI.
func (w *Wrapper) emitToolResponse(toolName string, responseStr string, err error) {
	if w.EventChan == nil {
		return
	}
	w.EventChan <- agui.AgentToolResponseMsg{
		Type:     agui.EventToolCallResult,
		ToolName: toolName,
		Response: responseStr,
		Error:    err,
	}
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

// writableFileTools maps write tool names to the JSON key containing the
// file path that should be invalidated in the file cache.
var writableFileTools = map[string]string{
	"write_file":   "file_name",
	"edit_file":    "file_name",
	"replace_file": "file_name",
}

// invalidateFileCacheOnWrite evicts any cached read_file result for the
// same path when a write tool modifies a file. Without this, subsequent
// read_file calls would return stale content from WorkingMemory.
func (w *Wrapper) invalidateFileCacheOnWrite(toolName string, jsonArgs []byte) {
	if w.WorkingMemory == nil {
		return
	}
	pathKey, ok := writableFileTools[toolName]
	if !ok {
		return
	}
	path := gjson.GetBytes(jsonArgs, pathKey)
	if !path.Exists() {
		return
	}
	// Evict the cached read for this file by storing an empty sentinel.
	// WorkingMemory has no Delete — an empty value effectively invalidates
	// the entry since the cached reader returns "" which differs from the
	// original file content.
	cacheKey := fmt.Sprintf("tool:read_file:%s", path.String())
	w.WorkingMemory.Store(cacheKey, "")
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
// Caller must hold w.callMu.
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
// Caller must hold w.callMu.
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

// approvalFingerprint produces a deterministic cache key for the session-scoped
// approval cache. It hashes the threadID (session), tool name, and arguments
// so that the same tool+args combination in the same session is auto-approved.
func approvalFingerprint(threadID, toolName, args string) string {
	h := sha256.New()
	h.Write([]byte(threadID))
	h.Write([]byte("|"))
	h.Write([]byte(toolName))
	h.Write([]byte("|"))
	h.Write([]byte(args))
	return fmt.Sprintf("%x", h.Sum(nil))
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

// maxApprovalCacheSize limits the number of entries in the approval cache
// to prevent unbounded memory growth from many unique tool+args combinations.
const maxApprovalCacheSize = 256

// storeApproval adds an approval key to the bounded cache. When full, the
// oldest entry is evicted (FIFO). This prevents a user or LLM from growing
// memory without limit by generating many unique tool+args combinations.
func (w *Wrapper) storeApproval(key string) {
	w.approvalMu.Lock()
	defer w.approvalMu.Unlock()
	if w.approvalCache == nil {
		w.approvalCache = make(map[string]struct{})
	}
	if _, exists := w.approvalCache[key]; exists {
		return
	}
	if len(w.approvalOrder) >= maxApprovalCacheSize {
		evict := w.approvalOrder[0]
		w.approvalOrder = w.approvalOrder[1:]
		delete(w.approvalCache, evict)
	}
	w.approvalCache[key] = struct{}{}
	w.approvalOrder = append(w.approvalOrder, key)
}

// sensitiveKeys lists JSON key substrings whose values are redacted in audit
// logs to prevent accidental retention of secrets and credentials.
var sensitiveKeys = []string{
	"token", "password", "secret", "api_key",
	"credentials", "authorization",
}

// maxAuditArgBytes caps the audit arg string to prevent large payloads from
// inflating audit log size.
const maxAuditArgBytes = 4096

// redactSensitiveArgs returns a sanitised version of the tool arguments for
// audit logging. Keys whose names contain common secret substrings have their
// values replaced with [REDACTED]. Walks the JSON structure recursively to
// catch secrets in nested objects. The result is capped at maxAuditArgBytes.
func redactSensitiveArgs(args []byte) string {
	if len(args) == 0 {
		return ""
	}
	redacted := string(args)
	// Recursive walk: collect all paths that need redaction.
	var redactPaths []string
	var walkJSON func(prefix string, result gjson.Result)
	walkJSON = func(prefix string, result gjson.Result) {
		if result.Type != gjson.JSON {
			return
		}
		result.ForEach(func(key, value gjson.Result) bool {
			fullPath := key.String()
			if prefix != "" {
				fullPath = prefix + "." + key.String()
			}
			k := strings.ToLower(key.String())
			for _, s := range sensitiveKeys {
				if strings.Contains(k, s) {
					redactPaths = append(redactPaths, fullPath)
					return true // don't recurse into redacted values
				}
			}
			if value.Type == gjson.JSON {
				walkJSON(fullPath, value)
			}
			return true
		})
	}
	walkJSON("", gjson.Parse(redacted))

	for _, p := range redactPaths {
		if r, err := sjson.Set(redacted, p, "[REDACTED]"); err == nil {
			redacted = r
		}
	}
	if len(redacted) > maxAuditArgBytes {
		redacted = fmt.Sprintf(`{"_truncated":true,"_original_bytes":%d}`, len(redacted))
	}
	return redacted
}

// semanticKeyFields maps tool names to the JSON fields that form the semantic
// identity of a call. When present, only these fields are used for
// deduplication instead of the full argument blob. This prevents re-executing
// idempotent tools when the LLM varies non-key arguments across iterations.
var semanticKeyFields = map[string][]string{
	"create_recurring_task": {"name"},
}

// semanticKey builds a dedup key for tools that have semantic key fields
// configured. Returns the key and true if the tool is eligible for semantic
// dedup, or ("", false) otherwise.
func semanticKey(toolName string, jsonArgs []byte) (string, bool) {
	fields, ok := semanticKeyFields[toolName]
	if !ok {
		return "", false
	}
	key := toolName
	for _, f := range fields {
		val := gjson.GetBytes(jsonArgs, f)
		if !val.Exists() {
			return "", false // required key field missing
		}
		key += ":" + val.String()
	}
	return key, true
}
