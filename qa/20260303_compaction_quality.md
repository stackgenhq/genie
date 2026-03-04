# 🧪 Test Suite: Compaction Quality Feedback

**File**: `20260303_compaction_quality.md`

## 🎯 Purpose

To verify that Genie's context-mode compression (BM25-based chunk scoring in `mw_contextmode.go`) correctly tracks which tool outputs were compressed, detects when the agent re-invokes the same tool due to lost information, fires the `CompactionMiss` hook, and (when adaptive compaction is enabled) automatically increases `max_chunks` on retry to recover the missing data.

## 🛠 Setup

1. **Build Genie**:
   ```bash
   make only-build
   ```

2. **Start the server** with context-mode enabled and a low threshold (to force compression on moderate outputs):
   Create a test config `qa/compaction_quality_demo/genie.toml`:
   ```toml
   agent_name = "compaction_qa"

   [[model_config.providers]]
   provider = "gemini"
   model_name = "gemini-3-flash-preview"
   token = "${GEMINI_API_KEY}"
   good_for_task = "tool_calling"

   [messenger.agui]
   port = 9876
   cors_origins = ["*"]

   [context_mode]
   enabled = true
   threshold = 5000
   max_chunks = 3
   ```

3. **Export env vars and start**:
   ```bash
   set -a && source .env && set +a
   cd qa/compaction_quality_demo
   ../../build/genie grant --config genie.toml
   ```

4. **Open Chat UI**: http://localhost:9876/ui/chat.html

---

## 🧪 Scenarios

### Scenario 1: Compression Tracking — `MarkCompressed` fires

**Goal**: Verify that when a tool output exceeds the threshold, the middleware logs compression details and calls `MarkCompressed`.

1. **Send prompt**: "Read the file /etc/hosts and explain its contents." (or any tool call that produces output > 5000 chars)
2. **Check server logs** for:
   - `"tool response exceeds threshold, applying context-mode compression"` — indicates the middleware activated
   - `"context-mode compression complete"` with fields `compressed_chars`, `chunks_kept`, `chunks_total`, `compression_ratio`
3. **Expected**: Both log lines present. The `chunks_kept` should equal `max_chunks` (3).

### Scenario 2: Normal completion — no compaction miss

**Goal**: Verify that when the agent receives compressed output and proceeds normally (doesn't re-invoke the same tool), no `CompactionMiss` warning is emitted.

1. **Send prompt**: "List all files in the /tmp directory and summarize."
2. **Check server logs**: Should NOT contain `"possible compaction miss"`.
3. **Expected**: The agent uses the compressed output and moves on. No compaction miss warning.

### Scenario 3: Compaction miss detection

**Goal**: Verify that when the agent re-invokes the same tool with the same arguments after receiving compressed output, the `CompactionMiss` hook fires.

> **Note**: This scenario is hard to trigger deterministically via the chat UI because it requires the LLM to repeat an identical tool call. The primary verification is through unit tests (see Automated Tests below). If you want to attempt it manually:

1. **Send prompt**: Ask Genie to find a very specific string in a large file. Use a query where the relevant content is likely to be in a chunk that gets dropped by the BM25 scoring (e.g., a string that has no overlap with the tool's input arguments).
2. **Check server logs** for:
   - `"possible compaction miss — agent re-invoked tool identically after compressed output"`
   - The log should include the `tool` name.
3. **Expected**: Warning logged and `OnCompactionMiss` hook fired.

### Scenario 4: Adaptive compaction — `max_chunks` auto-increases on retry

**Goal**: Verify that after a compaction miss is detected, the next invocation of the same tool uses an increased `max_chunks` value, giving the agent more content to work with.

> **Note**: Like Scenario 3, this is primarily verified through unit tests. Manual verification requires observing the `max_chunks` value in logs across iterations.

1. **Trigger Scenario 3** (compaction miss).
2. **On the retry iteration**, check server logs for a higher `chunks_kept` value compared to the first invocation.
3. **Expected**: `chunks_kept` should be greater than the original `max_chunks` (3), indicating the adaptive increase took effect.

---

## ✅ Automated Tests

### Unit Tests (primary verification)

These tests run without any API keys or running server.

```bash
# Run compaction middleware tests
go tool ginkgo -v -mod=mod --race ./pkg/toolwrap/ -- -ginkgo.focus="ContextModeMiddleware"

# Run adaptive loop / compaction miss tests
go tool ginkgo -v -mod=mod --race ./pkg/reactree/ -- -ginkgo.focus="compaction"

# Run hook tests
go tool ginkgo -v -mod=mod --race ./pkg/hooks/
```

#### Key test cases to verify:

**In `pkg/toolwrap/mw_contextmode_test.go`**:
- ✅ `"invokes CompactionTracker.MarkCompressed when tracker is in context"` — verifies the middleware calls `MarkCompressed` with correct tool name, original size, and compressed size.

**In `pkg/reactree/tree_test.go`** (to be added):
- `"detects compaction miss when tool is re-invoked identically after compression"` — verifies `OnCompactionMiss` hook fires.
- `"adaptive compaction increases max_chunks after compaction miss"` — verifies the retry uses a larger `max_chunks`.

**In `pkg/hooks/hooks_test.go`**:
- ✅ `"should not panic on any method including OnContextBudget and OnCompactionMiss"` — verifies NoOpHook and ChainHook handle `CompactionMissEvent`.

---

## ✅ Acceptance Criteria

- [ ] BM25 compression activates and logs structured output when tool response exceeds threshold.
- [ ] `MarkCompressed` is called with correct (tool name, original size, compressed size).
- [ ] `CompactionMiss` hook fires when the same tool is re-invoked identically after compressed output.
- [ ] Adaptive compaction increases `max_chunks` for the tool on retry after a compaction miss.
- [ ] No false positives: compaction miss does NOT fire when different tools are called or args differ.
- [ ] Per-tool overrides still work correctly with adaptive compaction.
