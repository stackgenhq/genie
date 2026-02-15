# Audit Logging — Acceptance Criteria

> Tests for all audit log event types: classification, LLM request/response, tool calls, and conversation turns.

---

## Audit Log: Classification Events

### Why
Classification events record how each user request was categorised (salutation, complex, refuse). This is essential for debugging routing decisions.

### Problem
Without classification audit events, there is no way to trace why a request was routed to a particular handler.

### Benefit
Enables post-hoc analysis of request routing and helps identify misclassifications.

### Arrange
- Completed Tests 3 and 4

### Act
```bash
cat audit.log | grep '"event_type":"classification"'
```

### Assert
- At least one event with `"action":"SALUTATION"` or `"action":"COMPLEX"`
- Each event has:
  - `"actor":"front-desk"`
  - `"metadata.question"` containing the user's original question
  - `"metadata.sender_context"` set to `"agui:http"`

---

## Audit Log: LLM Request/Response Events

### Why
LLM call events track every model invocation, providing visibility into latency, token usage, and which models are being called.

### Problem
Without these events, debugging slow responses or model failures requires manual log analysis.

### Benefit
Provides structured telemetry for LLM calls, enabling performance monitoring and cost analysis.

### Arrange
- Completed any chat interaction

### Act
```bash
cat audit.log | grep '"event_type":"llm_request"' | head -1
cat audit.log | grep '"event_type":"llm_response"' | head -1
```

### Assert
- `llm_request` events have:
  - `"actor"` = `"front-desk"` or `"genie"`
  - `"action"` = `"llm_call_started"`
  - `"metadata.session_id"` (UUID)
  - `"metadata.task_type"` (e.g., `"frontdesk"` or `"general_task"`)
  - `"metadata.message"` (contains the prompt text)
- `llm_response` events have:
  - `"action"` = `"llm_call_completed"`
  - `"metadata.duration_ms"` (numeric, > 0)
  - `"metadata.choice_count"` (numeric, ≥ 1)

---

## 8 — Audit Log: Tool Call Events

### Why
Tool call events record every tool invocation, including arguments and results. This is critical for auditing agent actions.

### Problem
Without tool call events, there is no audit trail of what the agent actually *did* (vs. what it *said*).

### Benefit
Enables compliance auditing, debugging, and replay of agent actions.

### Arrange
- Completed Test 4 (a request that invokes tools)

### Act
```bash
cat audit.log | grep '"event_type":"tool_call"'
```

### Assert
- At least one `tool_call` event exists
- Each event has:
  - `"actor"` = `"expert"`
  - `"action"` = the tool name (e.g., `"list_file"`, `"read_multiple_files"`)
  - `"metadata.args"` (JSON string of tool arguments)
  - `"metadata.response_length"` (numeric)
  - `"metadata.truncated"` (boolean)
  - `"metadata.error"` (empty string on success)

---

## 9 — Audit Log: Conversation Turn Events

### Why
Conversation turn events capture the full Q&A pair, enabling conversation replay and quality analysis.

### Problem
Without turn-level events, reconstructing what happened in a session requires correlating multiple lower-level events.

### Benefit
Provides a single event per turn that summarises the interaction, useful for quality monitoring and debugging.

### Arrange
- Completed any chat interaction

### Act
```bash
cat audit.log | grep '"event_type":"conversation"'
```

### Assert
- At least one event with `"action":"chat_turn_completed"`
- Each event has:
  - `"actor"` = `"code-owner"`
  - `"metadata.question"` (the user's input)
  - `"metadata.answer"` (the assistant's full response, may be truncated)
  - `"metadata.sender_context"` = `"agui:http"`
