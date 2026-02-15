# Chat Flows — Acceptance Criteria

> Tests for greeting/salutation handling, tool-based file operations, follow-up memory, and UI indicator behaviour.

---

## 3 — Greeting (Salutation Flow)

### Why
Simple greetings should be handled efficiently without invoking heavy tool chains, and the response must not hallucinate file listings.

### Problem
Early implementations used the classifier prompt for salutations, causing fabricated environment details to leak into replies.

### Benefit
Users get a natural, conversational greeting that accurately describes capabilities without false claims.

### Arrange
- Connected to the server (Test 2)

### Act
Type and send: `Hello! What can you do?`

### Assert
- The assistant responds conversationally (e.g., lists capabilities)
- The response does **NOT** hallucinate specific file names or directory listings
- No error bubbles appear
- The thinking indicator disappears after the response is complete

---

## 4 — File Operations (Complex Flow with Tools)

### Why
File operations are a core capability of Genie. This tests the full tool-call pipeline from request classification through execution.

### Problem
Without this test, regressions in tool invocation, event streaming, or result rendering would go undetected.

### Benefit
Validates that the agent can autonomously use tools and present structured results to the user.

### Arrange
- Connected to the server (Test 2)

### Act
Type and send: `List the files in the cmd directory and tell me what each one does`

### Assert
- One or more **tool cards** appear (e.g., `🔧 list_file`) during execution
- Tool cards transition from `⏳ Running…` to `✅ Done`
- The assistant provides an accurate list of files in `cmd/` with descriptions
- The response includes at least: `root.go`, `grant.go`, `connect.go`, `version.go`
- The **thinking indicator** (e.g., "Executing…", "Reviewing…") disappears when the response is complete

---

## 5 — Follow-up Conversation (Memory)

### Why
Conversation continuity is essential for multi-turn interactions. Users expect the agent to remember prior context.

### Problem
Without memory, every follow-up question would require the user to re-provide context, making the experience frustrating.

### Benefit
Enables natural, flowing conversations where earlier tool results inform later answers.

### Arrange
- Completed Test 4 (file listing is in conversation history)

### Act
Type and send: `Which of between root.go and grant.go is more important and why?`

### Assert
- The assistant references the files from the **previous** response (demonstrates memory)
- It does **NOT** need to re-list the directory
- The answer is contextually coherent (e.g., says `grant.go` is the main server logic)

---

## 10 — Thinking Indicator Clears on Completion

### Why
A stale thinking indicator makes the UI feel broken and confuses users about whether the agent is still working.

### Problem
If the indicator remains after a response completes, users may wait indefinitely or lose trust in the interface.

### Benefit
Clear visual feedback that the agent has finished, and the input is ready for the next message.

### Arrange
- Connected to the server (Test 2)

### Act
1. Send a complex query (e.g., `List files in pkg/audit`)
2. Wait for the full response to appear

### Assert
- After the response is fully displayed, **no** thinking indicator (e.g., "Thinking…", "Executing…", "Reviewing…") remains visible
- The chat input is re-enabled (not greyed out)
- The send button is clickable
