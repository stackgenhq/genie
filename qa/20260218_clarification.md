# Clarification Tool — Acceptance Criteria

> Tests for the `ask_clarifying_question` tool: ambiguous-request triggering, inline UI card rendering, and user-response resumption.

---

## 20 — Clarification: Ambiguous Request Triggers Question

### Why
Genie must ask clarifying questions instead of guessing when a request is ambiguous or missing critical details. This is the core value proposition of the clarification tool.

### Problem
Without this, the agent would silently make assumptions — potentially executing destructive or irrelevant actions.

### Benefit
Validates that the orchestrator (persona prompt) and sub-agents (`create_agent` instruction) correctly invoke `ask_clarifying_question` for ambiguous inputs.

### Arrange
- Server running with default configuration
- Open `docs/chat.html` in a browser, connect

### Act
1. Send: `Deploy the app`
2. Wait for the agent to process the request

### Assert
- A clarification card (yellow/amber) appears in the chat asking the user for details (e.g., which environment, which app, which method)
- The card contains an input field for the user's response
- The agent does **not** proceed with any tool calls before receiving a clarification response

---

## 21 — Clarification: User Response Resumes Agent

### Why
After asking a clarifying question, the agent must accept the user's response and continue execution with the clarified context.

### Problem
If the response loop is broken, the agent would hang after asking a question — requiring the user to restart the conversation.

### Benefit
Validates the full clarification round-trip: question → user input → agent resumes with context.

### Arrange
- Server running with default configuration
- Open `docs/chat.html` in a browser, connect
- A clarification card is visible from a previous ambiguous request

### Act
1. Type a response in the clarification input field (e.g., `Deploy to staging using Docker`)
2. Click the **Submit** button (or press Enter)

### Assert
- The clarification card updates to show the user's response (✅ Answered)
- The agent resumes execution using the clarified context
- Subsequent tool calls reflect the clarified details (e.g., staging environment, Docker)
- No error messages or hangs occur
