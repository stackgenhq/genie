# TUI Package — Architecture & Debugging Guide

## Event Pipeline

The TUI communicates with the agent through a two-stage event pipeline:

```
┌───────────────┐     ┌──────────────┐     ┌──────────────┐     ┌───────┐
│ Agent / Expert│────▶│ rawEventChan │────▶│ EventAdapter │────▶│ Model │
│ (grant.go)    │     │ (chan iface{})│     │ (goroutine)  │     │(Update)│
└───────────────┘     └──────────────┘     └──────────────┘     └───────┘
                                                                    │
                                                            ┌───────┴───────┐
                                                            │   ChatView    │
                                                            │  (completed)  │
                                                            └───────────────┘
```

### Key channels

| Channel | Direction | Purpose |
|---------|-----------|---------|
| `rawEventChan` | Agent → Adapter | Raw events (`event.Event`, `AgentChatMessage`, `LogMsg`, etc.) |
| `tuiEventChan` | Adapter → Model | Converted TUI messages (`AgentStreamChunkMsg`, `AgentToolCallMsg`, etc.) |
| `inputChan` | Model → Agent | User chat input (strings) |

### EventAdapter conversion rules

| Raw event type | Converted to | Notes |
|---------------|-------------|-------|
| `event.Event` with `ToolID` | `AgentToolResponseMsg` | Tool result — `continue` (skip content) |
| `event.Event` with `Content` | `AgentStreamChunkMsg` | Skipped if `ToolCalls` present |
| `event.Event` with `ToolCalls` | `AgentToolCallMsg` | One per tool call |
| `event.Event` with `FinishReason` | `AgentErrorMsg` | Only for error/filter/length |
| `event.Event` with `Error` | `AgentErrorMsg` | API-level errors |
| `AgentChatMessage` | passthrough | Not an `event.Event` |
| `AgentThinkingMsg` | passthrough | Not an `event.Event` |
| `LogMsg` | passthrough | Not an `event.Event` |
| `StageProgressMsg` | passthrough | Not an `event.Event` |

## Chat Mode Response Flow

During chat (`state.Completed == true`), LLM responses arrive via **two redundant paths**:

```
Path 1 (EventChannel):
  expert.Do() → event.Event → rawEventChan → adapter → AgentStreamChunkMsg → chatView.AppendToLastMessage()
  ✅ This is the primary path for rendering

Path 2 (ChoiceProcessor → outputChan):
  ChoiceProcessor callback → outputChan → grantWithTUI loop → EmitAgentMessage → AgentChatMessage
  ⚠️ This is kept alive by waitForEvent only — NOT used for display
```

> [!IMPORTANT]
> Every `case` in `Model.Update` that handles an event from `eventChan` **MUST** return
> `waitForEvent(m.eventChan)` as a command. Without it, the event loop stops and all
> subsequent events are silently dropped. This was the cause of the "no output in chat" bug.

## Message Types in ChatView

| Role | Source | Rendering |
|------|--------|-----------|
| `"user"` | User input | Purple bubble, right-aligned |
| `"model"` | LLM response (via `AgentStreamChunkMsg`) | Gray bubble, left-aligned, markdown rendered |
| `"system"` | Slash commands, `/help`, errors | Cyan border, left-aligned |
| `"tool"` | `AddToolCall()` / `UpdateToolCall()` | Tool card with status icon, color-coded diffs |

## Tool Call Lifecycle

```
1. AgentToolCallMsg received
   → chatView.AddToolCall(msg)
   → Creates toolCallState{Status: "running"}
   → Renders: 🔧 read_file main.tf ⟳

2. AgentToolResponseMsg received
   → chatView.UpdateToolCall(msg)
   → Updates status to "done" or "error"
   → Renders: 🔧 read_file main.tf ✓ 41 lines read

3. Diff previews for write operations:
   → extractDiffPreview() generates + / - lines
   → colorDiffLines() applies DiffAdd (green) / DiffRemove (red) styles
```

## Slash Commands

Handled locally in `ChatView.handleSlashCommand()` — never sent to the agent:

| Command | Handler |
|---------|---------|
| `/help` | Shows available commands as system message |
| `/clear` | Resets chat messages |

## Common Debugging Scenarios

### "No output in chat window"
1. Check that `Model.Update` handler for the event type calls `waitForEvent(m.eventChan)`
2. Check that `EventAdapter.ConvertEvent()` produces the expected message type
3. Check that `chatView.AppendToLastMessage()` finds a "model" message to append to (or creates one)
4. Look at log panel — if tool calls appear there but not in chat, the routing in `Model.Update` may check `m.state.Completed` incorrectly

### "Tool cards stuck on ⟳"
1. Verify `EventAdapter` converts tool response events (`choice.Message.ToolID != ""`) to `AgentToolResponseMsg`
2. Check `Model.Update` routes `AgentToolResponseMsg` to `chatView.UpdateToolCall()` when completed
3. Verify `ToolCallID` matches between the call and response events

### "Duplicate messages"
1. Check if both `AgentChatMessage` (ChoiceProcessor path) and `AgentStreamChunkMsg` (EventChannel path) are adding content
2. `AgentChatMessage` should only maintain the event loop, NOT add to display (content already comes via `AgentStreamChunkMsg`)

## File Map

| File | Responsibility |
|------|---------------|
| `model.go` | Top-level MVU model, event routing, state management |
| `chat_view.go` | Chat UI, message rendering, slash command interception, tool cards |
| `agent_view.go` | Agent progress view (pre-completion), typing animation |
| `adapter.go` | Converts `event.Event` → TUI message types |
| `events.go` | TUI message type definitions |
| `helpers.go` | `RunGrantWithTUI`, `Emit*` helpers, TUI logger |
| `styles.go` | All Lip Gloss styles (bubbles, tool cards, diffs) |
| `slash_commands.go` | Slash command definitions and handlers |
| `tool_cards.go` | Tool call state, arg/result summarizers, diff extraction |
| `runner.go` | Simple `RunWithTUI` (no adapter, direct channel) |
| `types.go` | `ChatMessage`, `LogEntry` types |
