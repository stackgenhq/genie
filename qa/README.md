# 🧪 Genie QA — Acceptance Criteria

This directory contains the acceptance criteria for the **Genie** chat server. Every feature ships with a structured test plan that anyone on the team can execute independently — no tribal knowledge required.

Each test follows the **Arrange / Act / Assert** pattern and documents:

- **Why** the feature was built
- **What problem** it solves
- **How** it benefits users or the system
- **How to validate** it (step-by-step)

> New here? Start with [Server & Connectivity](20260214_server_connectivity.md) to verify the server is running, then work through the suites in order.

---

## Prerequisites

| What | How |
|------|-----|
| Build the binary | `make only-build` (produces `build/genie`) |
| API keys | Ensure `.env` exists with at least `OPENAI_API_KEY` and one LLM key (`GEMINI_API_KEY` or `ANTHROPIC_API_KEY`) |
| Export env vars | `set -a && source .env && set +a` — plain `source .env` does **not** export vars to child processes |
| Config file | Create `.genie.toml` in the repo root with `[messenger.agui]` section, `port = 9876`, `cors_origins = ["*"]` |
| Chat UI | Open `docs/chat.html` in a browser |

### Minimal Config

Create `.genie.toml` in the repo root with at least:

```toml
agent_name = "qa_agent"

# Model providers — use correct model names (see table below)
[[model_config.providers]]
provider = "gemini"
model_name = "gemini-3-flash-preview"
token = "${GEMINI_API_KEY}"
good_for_task = "tool_calling"

[messenger.agui]
port = 9876
cors_origins = ["*"]

[hitl]
always_allowed = ["read_file", "list_file"]
```

### Supported Model Names

These are the **default model names** recognised by the code (defined in `pkg/expert/modelprovider/model.go`). Using other names will cause echo-check failures at startup.

| Provider | Default Model Name | Env Var for Token |
|----------|-------------------|-------------------|
| `openai` | `gpt-4o` | `OPENAI_API_KEY` |
| `gemini` | `gemini-3-flash-preview` | `GEMINI_API_KEY` or `GOOGLE_API_KEY` |
| `anthropic` | `claude-sonnet-4-6` | `ANTHROPIC_API_KEY` |
| `ollama` | `llama3` | _(none — local)_ |

### Starting the Server

```bash
# Export env vars (critical — plain source does not export)
set -a && source .env && set +a

# Start the server
./build/genie grant
```

Verify with: `curl http://localhost:9876/health` → HTTP 200.

### Running genie doctor

```bash
set -a && source .env && set +a
./build/genie doctor
```

All checks should pass. If model providers fail, check that tokens are exported and model names match the table above.

---

## Test Suites

| Feature Area | File |
|---|---|
| Server & Connectivity | [20260214_server_connectivity.md](20260214_server_connectivity.md) |
| Chat UI (connect, mic, read aloud, fullscreen, shortcuts) | [20260226_chat_ui.md](20260226_chat_ui.md) |
| Chat Flows | [20260214_chat_flows.md](20260214_chat_flows.md) |
| Audit Logging | [20260214_audit_logging.md](20260214_audit_logging.md) |
| Summarizer | [20260214_summarizer.md](20260214_summarizer.md) |
| Human-in-the-Loop (HITL) | [20260214_hitl.md](20260214_hitl.md) |
| Embedding Providers (HuggingFace & Gemini) | [20260217_embedding_providers.md](20260217_embedding_providers.md) |
| Runbooks | [20260217_runbooks.md](20260217_runbooks.md) |
| Activity Report (cron) | [20260227_activity_report.md](20260227_activity_report.md) |
| MCP validation | [20260227_mcp_validation.md](20260227_mcp_validation.md) |
| genie doctor | [20260227_genie_doctor.md](20260227_genie_doctor.md) |
| Knowledge graph + data sources | [20260227_graph_datasource.md](20260227_graph_datasource.md) |
| Dynamic Skills Loading | [20260303_dynamic_skills.md](20260303_dynamic_skills.md) |

---

## Available Commands

| Command | Purpose |
|---------|---------|
| `genie grant` | Start the AG-UI server (main entry point) |
| `genie doctor` | Validate config, model providers, MCP, SCM, secrets |
| `genie connect` | Connect to a running AG-UI server |
| `genie setup` | Interactive config setup (3-step wizard) |
| `genie version` | Show build version |
| `genie back-to-bottle` | Clean up keyring and local secret data |

> **Note:** `genie mcp validate` is documented in [20260227_mcp_validation.md](20260227_mcp_validation.md) but is **not a separate subcommand** in the current build. MCP server checks are part of `genie doctor`.

---

## Audit Log Location

Audit logs are written to `~/.genie/<agent_name>.<yyyy_mm_dd>.ndjson`. For example, with `agent_name = "qa_agent"`, today's log is:

```
~/.genie/qa_agent.2026_03_02.ndjson
```

Query events with:
```bash
cat ~/.genie/qa_agent.*.ndjson | grep '"event_type":"tool_call"' | head -5
```

---

## Known Gaps (Not Yet Implemented)

The following audit event types are defined in `pkg/audit/audit.go` but are **not yet emitted** by any code path:

| Event Type | Description | Status |
|---|---|---|
| `connection` | Logged when a client connects | ❌ Not wired |
| `disconnection` | Logged when a client disconnects | ❌ Not wired |
| `command` | Logged when a shell command is executed | ❌ Not wired |

These should be implemented in future work and added as acceptance tests once they are.

---

## Known Issues

| Issue | Description | Workaround |
|-------|-------------|------------|
| OpenAI/Anthropic echo-check failures | At startup, `max_tokens` sent by the echo check exceeds API limits (e.g. `114687 > 16384` for gpt-4o). This is a code bug in internal model metadata, not a config problem. | The server still starts — Gemini provider typically passes and the server falls back. Ignore the WARN logs. |
| `genie mcp validate` missing | The MCP validation subcommand referenced in QA docs does not exist as a standalone command. | Use `genie doctor` instead — it includes MCP server checks. |
