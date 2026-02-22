# AGUI Refactor: Move HTTP Server to `pkg/messenger/agui`

## Goal
Everything HTTP-related is strictly AGUI-only. The HTTP SSE server should only
bootstrap when `messenger.Platform == AGUI`. HITL, clarifications, etc. flow
through each messenger's native mechanisms.

## Current State

### `pkg/agui/` — mixed concerns (21 files)

**Shared event infrastructure** (used by reactree, toolwrap, expert, cron, app):
| File | Purpose | Importers |
|------|---------|-----------|
| `messages.go` | Event/message types (AgentThinkingMsg, AgentToolResponseMsg, etc.) | reactree, toolwrap, expert, cron, app |
| `context.go` | Context helpers (ThreadID, RunID, EventChan) | reactree, toolwrap, app |
| `helpers.go` | Emit helpers (EmitError, EmitAgentMessage, EmitThinking) | reactree, app |
| `agui_envelope.go` | CloudEvent wrapper | — |
| `types.go` | LogEntry type | — |

**Server infrastructure** (AGUI-only):
| File | Purpose |
|------|---------|
| `server.go` | HTTP server, routes, handleRun SSE, handleApprove, CORS, docs proxy |
| `server_expert.go` | NewChatHandlerFromCodeOwner (dedup, streaming bridge) |
| `adapter.go` | EventAdapter (trpc→TUI), not to be confused with messenger adapter |
| `events.go` | BackgroundWorker, EventRequest, EventType + handleEventsEndpoint/handleResumeEndpoint |
| `event_mapper.go` | MapEvent (internal msg → AG-UI wire JSON) |
| `runner_adapter.go` | NewRunner (framework runner.Runner wrapper) |
| `ratelimit.go` | Rate limit, concurrency limit, body size middleware |

### `pkg/messenger/agui/` — messenger adapter
| File | Purpose |
|------|---------|
| `agui.go` | Messenger interface (Connect/Disconnect/Send/Receive/InjectMessage/CompleteThread) |
| `sse.go` | SSEWriter (recently moved here) |

## Plan

### Phase 1: Split `events.go` (mixed shared + server code)

`events.go` currently contains both:
- **Shared types**: `EventType`, `EventRequest` (used by cron) — stay in `pkg/agui`
- **Server code**: `BackgroundWorker`, `handleEventsEndpoint`, `handleResumeEndpoint` — move out

Split `events.go`:
- Keep `EventType`, `EventRequest` in `pkg/agui/events.go` (already there alongside message types)
- Move `BackgroundWorker` + HTTP handlers to server files that move to `pkg/messenger/agui`

### Phase 2: Move server files to `pkg/messenger/agui`

Move these files (changing `package agui` is already correct):
1. `server.go` → `pkg/messenger/agui/server.go`
2. `server_expert.go` → `pkg/messenger/agui/server_expert.go`
3. `adapter.go` → `pkg/messenger/agui/adapter.go`
4. `event_mapper.go` → `pkg/messenger/agui/event_mapper.go`
5. `runner_adapter.go` → `pkg/messenger/agui/runner_adapter.go`
6. `ratelimit.go` → `pkg/messenger/agui/ratelimit.go`
7. `BackgroundWorker` from `events.go` → `pkg/messenger/agui/worker.go`
8. `handleEventsEndpoint`/`handleResumeEndpoint` → stays with `server.go`
9. Move corresponding test files

Each moved file will:
- Keep `package agui` (same package name)
- Add `import aguitypes "github.com/appcd-dev/genie/pkg/agui"` for shared event types
- Reference shared types as `aguitypes.AgentThinkingMsg`, etc.

### Phase 3: Update `pkg/agui` exports

What remains in `pkg/agui/`:
- `messages.go` — all message types and event constants
- `context.go` — context helpers
- `helpers.go` — emit helpers
- `agui_envelope.go` — CloudEvent wrapper
- `types.go` — LogEntry

These are purely types/functions with no server dependency. External consumers
(reactree, toolwrap, expert, cron) continue importing `pkg/agui` unchanged.

### Phase 4: Conditional bootstrap in `app.go`

Change `Application.Start()`:

```go
// Only start HTTP SSE server when AGUI is the messenger platform.
if a.msgr.Platform() == messenger.PlatformAGUI {
    // Create AGUI server, wire messenger bridge, set runner
    aguiServer := messengeragui.NewServer(...)
    // ...
    return aguiServer.Start(ctx)
}

// For external messengers: just run the receive loop + cron
a.startMessengerLoop(ctx)
// Block until context cancelled
<-ctx.Done()
return nil
```

### Phase 5: Move fakes and regenerate

- Move `aguifakes/` to `pkg/messenger/agui/aguifakes/`
- Regenerate counterfeiter fakes: `go generate ./pkg/messenger/agui/...`
- Move test files alongside their source

## Dependency Graph (after refactor)

```
pkg/agui  (shared event types, context helpers, emit helpers)
  ↑ imported by: reactree, toolwrap, expert, cron, app, pkg/messenger/agui

pkg/messenger/agui  (messenger adapter + HTTP server)
  → imports: pkg/agui (for event types)
  → imports: pkg/messenger (for AGUIConfig, messenger interfaces)
  ↑ imported by: app (via blank import for adapter registration + direct for server)
```

No circular imports — `pkg/agui` does NOT import `pkg/messenger/agui`.

## Risk Assessment
- **High impact**: ~30 files need import updates
- **Medium risk**: server.go references many same-package symbols that become cross-package
- **Mitigation**: Phase-by-phase execution with build verification at each step
