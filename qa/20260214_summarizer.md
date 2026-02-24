# Summarizer — Acceptance Criteria

> Tests for the summarizer feature: automatic condensation of large sub-agent outputs so responses stay concise and context windows remain manageable.

---

## 12 — Summarizer: Large Output Is Condensed

### Why
When a sub-agent produces a very large output (e.g., listing many files or reading a large codebase section), the summarizer should automatically condense it before presenting it to the user.

### Problem
Without summarisation, raw tool output can overwhelm the response and exhaust the context window, leading to degraded answers or failures.

### Benefit
Users receive concise, readable responses even when the underlying tool output is large.

### Arrange
- Server running (Test 1)
- Connected via `docs/chat.html` (Test 2)

### Act
Send a request that triggers a large tool output, e.g.:
```
List every file in the pkg/config directory recursively and describe what each one does
```

### Assert
- The response is a **concise summary**, not a raw dump of hundreds of file paths
- Tool cards appear showing the underlying tool calls executed
- The response completes without errors or truncation warnings

---

## 13 — Summarizer: Graceful Error Handling

### Why
When the summariser's upstream LLM call fails, the user should still receive a meaningful response rather than an opaque error or crash.

### Problem
If the summariser silently swallows errors or crashes, the user sees nothing useful after a long wait.

### Benefit
Users always get clear feedback, even when internal summarisation fails.

### Arrange
- Server running (Test 1)
- Connected via `docs/chat.html` (Test 2)

### Act
Send a request that triggers tool usage and observe the response:
```
List the files in the pkg/codeowner directory and summarise each one
```

### Assert
- The response arrives without the chat UI showing an error bubble or crashing
- If summarisation fails, the agent falls back gracefully (e.g., returns the raw output or an explanatory message)
- No hanging thinking indicators remain after the response

---

## 14 — Summarizer: Wired Into Sub-Agent Pipeline

### Why
The summariser must be properly integrated into the codeowner and create-agent pipelines so that large sub-agent outputs are automatically condensed.

### Problem
If the summariser is built but not wired, large sub-agent outputs are passed through raw, causing context-window exhaustion and degraded responses.

### Benefit
Sub-agent outputs exceeding the threshold are automatically summarised, keeping responses concise and context windows manageable.

### Arrange
- Server running (Test 1)
- Connected via `docs/chat.html` (Test 2)

### Act
Send a request that triggers a sub-agent with a large output, e.g.:
```
Explain the architecture of the pkg/reactree package in detail
```

### Assert
- The response is a coherent, summarised explanation — not a raw paste of source code
- Tool cards appear showing sub-agent tool calls
- The response completes without errors
