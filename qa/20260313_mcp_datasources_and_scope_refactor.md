# Feature: Generic Scope, MCP-Backed Datasources, and Linear Comments

## Why

This refactor makes the data source configuration **extensible**: adding a new source no longer requires modifying core types. It also introduces MCP-backed datasources (Jira, Confluence, ServiceNow) so Genie can index content from these systems via MCP server resources. Additionally, Linear issue comments are now included in synced content for richer context.

## Problem

- **Scope rigidity**: The `Scope` struct had hard-coded fields per source. Adding a new source meant modifying the struct, every constructor, and downstream code.
- **No MCP-as-datasource**: External systems like Jira, Confluence, and ServiceNow had no vectorization pathway.
- **Linear issues lacked comments**: Only issue title + description were synced; discussion in comments was lost.

## Benefit

- **Generic scope**: `Scope.Items` is a `map[string][]string` with `NewScope` / `Get`. New sources only need `SourceConfig`.
- **MCP datasources**: Jira, Confluence, and ServiceNow can be enabled as datasources via their MCP server. They read resources (`ListResources` / `ReadResource`) and produce `NormalizedItem`s for vectorization.
- **Linear comments**: Issue comments with author attribution are appended to synced content.

---

## Prerequisites

| What | How |
|------|-----|
| Build | `make only-build` |
| Root `.env` | Must contain `OPENAI_API_KEY`, `GEMINI_API_KEY`, `ANTHROPIC_API_KEY`, `PM_API_TOKEN`, `SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN` |
| MCP creds | Must contain `STACKGEN_MC_URL` and `STACKGEN_TOKEN` (for StackGen SSE MCP server providing Jira/Confluence/ServiceNow resources) |
| Export env | `set -a && source .env && set +a` |

### Minimal MCP + Datasource Config

Create or update `.genie.toml` in the repo root:

```toml
agent_name = "qa_datasource_agent"

[persona]
file = "./AGENTS.md"

[[model_config.providers]]
provider = "gemini"
model_name = "gemini-3-flash-preview"
token = "${GEMINI_API_KEY}"
good_for_task = "tool_calling"

[[model_config.providers]]
provider = "gemini"
model_name = "gemini-3-flash-preview"
token = "${GEMINI_API_KEY}"
good_for_task = "frontdesk"

# ── StackGen MCP server (SSE) — provides Jira/Confluence/ServiceNow resources ──
[[mcp.servers]]
name = "stackgen"
transport = "sse"
server_url = "${STACKGEN_MC_URL}"
[mcp.servers.headers]
Authorization = "Bearer ${STACKGEN_TOKEN}"

# ── Linear (PM) ──
[project_management]
provider = "linear"
api_token = "${PM_API_TOKEN}"

# ── Vector Memory ──
[vector_memory]
embedding_provider = "gemini"
persistence_dir = ".genie/memory"

# ── Data Sources ──
[data_sources]
enabled = true
sync_interval = "15m"

[data_sources.linear]
enabled = true
team_ids = ["TEAM-1"]

[data_sources.jira]
enabled = true
project_keys = ["PROJ"]
mcp_server = "stackgen"

[data_sources.confluence]
enabled = true
space_keys = ["ENG"]
mcp_server = "stackgen"

[data_sources.servicenow]
enabled = true
table_names = ["incident"]
mcp_server = "stackgen"

# ── AG-UI Server ──
[messenger.agui]
port = 9876
cors_origins = ["*"]

[hitl]
always_allowed = ["read_file", "list_file"]
```

> **Note on `mcp_server`**: The `mcp_server` field in each datasource config must match the `name` of a configured MCP server. Here all three use `"stackgen"` since the StackGen SSE endpoint provides resources for multiple systems.

---

## Test 1: MCP datasource sync (Jira via StackGen SSE)

### Arrange

- `.genie.toml` configured as above with `[data_sources.jira]` enabled and `mcp_server = "stackgen"`.
- `.env` contains `STACKGEN_MC_URL` and `STACKGEN_TOKEN`.

### Act

1. `set -a && source .env && set +a`
2. `./build/genie grant`
3. Wait for at least one sync interval (watch logs for `"source": "stackgen"` or `"jira"`).
4. In chat UI, ask: *"What are the recent Jira issues?"*

### Assert

1. Logs show sync attempt for the MCP datasource (e.g. `"source"`, `"items_listed"`, `"chunks_upserted"`).
2. No fatal errors related to MCP dial or `ListResources` / `ReadResource`.
3. If resources were listed, `memory_search` returns Jira-related content.

---

## Test 2: Confluence MCP datasource sync

### Arrange

- Same config with `[data_sources.confluence]` enabled, `space_keys = ["ENG"]`, `mcp_server = "stackgen"`.

### Act

1. Start Genie and wait for sync.
2. Ask: *"Summarize recent documentation from Confluence."*

### Assert

1. Logs show sync for Confluence via the stackgen MCP server.
2. If resources matched, `memory_search` surfaces them.

---

## Test 3: ServiceNow MCP datasource sync

### Arrange

- Same config with `[data_sources.servicenow]` enabled, `table_names = ["incident"]`, `mcp_server = "stackgen"`.

### Act

1. Start Genie and wait for sync.
2. Ask: *"What are the recent ServiceNow incidents?"*

### Assert

1. Logs show sync for ServiceNow source.
2. No MCP connection errors for the stackgen server.

---

## Test 4: `datasource_keyword_regex` filtering

### Arrange

- Add keyword regex to the StackGen MCP server config:
  ```toml
  [[mcp.servers]]
  name = "stackgen"
  transport = "sse"
  server_url = "${STACKGEN_MC_URL}"
  datasource_keyword_regex = ["INCIDENT-.*", "sprint"]
  [mcp.servers.headers]
  Authorization = "Bearer ${STACKGEN_TOKEN}"
  ```

### Act

1. Start Genie and wait for sync.

### Assert

1. Only resources whose URI or Name matches `INCIDENT-.*` or `sprint` are indexed.
2. Non-matching resources are silently skipped. Logs should show fewer `chunks_upserted` than total resources.

---

## Test 5: MCP datasource incremental sync (ListItemsSince)

### Arrange

- MCP datasource configured and at least one full sync has completed.
- `datasource_sync_state.json` exists with a last sync timestamp.

### Act

1. Restart Genie and wait for the next sync.

### Assert

1. Second sync only fetches resources with `lastModified` after the stored threshold.
2. Resources without `lastModified` are still included (conservative approach).
3. Items not changed since last sync are skipped (fewer `chunks_upserted`).

---

## Test 6: Linear issue comments in synced content

### Arrange

- Linear datasource enabled with `team_ids`. Linear has issues with ≥1 comment.
- Config includes `[project_management]` with Linear API token from `.env` (`PM_API_TOKEN`).

### Act

1. Start Genie and wait for sync.
2. Ask: *"What are the latest discussions on Linear issues?"*

### Assert

1. Logs show sync for Linear source.
2. Synced content includes comments (`--- Comments ---` section with author).
3. Metadata includes `comment_count`.

---

## Test 7: Multi-source sync (SCM + Linear + MCP)

This test is inspired by the `devops-copilot` and `production-incident-triage` examples which configure GitHub SCM + Linear + MCP servers.

### Arrange

- Config includes all of:
  ```toml
  [scm]
  provider = "github"
  token = "${GITHUB_TOKEN}"

  [project_management]
  provider = "linear"
  api_token = "${PM_API_TOKEN}"

  [data_sources]
  enabled = true

  [data_sources.linear]
  enabled = true
  team_ids = ["TEAM-1"]

  [data_sources.jira]
  enabled = true
  project_keys = ["PROJ"]
  mcp_server = "stackgen"
  ```

### Act

1. Start Genie and wait for sync.
2. Ask: *"Search across all my data sources for anything about deployment issues."*

### Assert

1. Logs show sync for multiple sources (e.g. `github`, `linear`, `stackgen`/`jira`).
2. `memory_search` returns results from multiple sources.
3. The agent synthesizes information across sources.

---

## Test 8: MCP server unreachable — graceful degradation

### Arrange

- Configure `mcp_server = "nonexistent"` in a datasource config (pointing to no actual MCP server).
- Or point `server_url` to an unreachable endpoint.

### Act

1. Start Genie.

### Assert

1. Startup is **not blocked** indefinitely (refer to [BUG-001](bugs/BUG-001-mcp-sse-timeout-blocks-startup.md) for known issues).
2. Datasource sync for the misconfigured source logs a warning but does not crash.
3. Other sources continue to sync normally.

---

## Test 9: Generic Scope — code-level verification

### Assert (Code Review)

1. No connector references hard-coded scope fields (e.g. `scope.GmailLabelIDs`).
2. All connectors call `scope.Get("sourceName")` where `sourceName` matches their `Name()`.
3. Tests use `datasource.NewScope("sourceName", values)` to construct scopes.

---

## Documenting Bugs

Any bugs found during testing must be documented in `qa/bugs/` following the established format.

### Bug File Format

- **Filename**: `BUG-NNN-short-description.md` (next available: `BUG-008`)
- **Template**:

```markdown
# BUG-NNN: Short Title

**Severity**: Critical / High / Medium / Low
**Component**: e.g. MCP Datasource / Linear Connector / Scope API
**Date**: YYYY-MM-DD

## Description
One paragraph describing the bug.

## Steps to Reproduce
1. Step one
2. Step two

## Expected Behavior
What should happen.

## Actual Behavior
What actually happens.

## Server Log Evidence
```
relevant log lines
```

## Impact
User/system impact.

## Suggested Fix
Brief suggestion.
```

### Existing Bugs (reference)

| ID | Title |
|----|-------|
| BUG-001 | MCP SSE Connection Timeout Blocks Startup |
| BUG-002 | Memory Search Exploration Loop |
| BUG-003 | Greeting Classified as Complex |
| BUG-004 | HITL Approve Returns 404 Not 500 |
| BUG-005 | Audit Conversation Actor Mismatch |
| BUG-006 | Context Cancellation Error Leaks |
| BUG-007 | Chat UI No Auto Connect |

---

## Troubleshooting

| Issue | Check |
|-------|-------|
| MCP dial failure | Ensure `STACKGEN_MC_URL` and `STACKGEN_TOKEN` are exported. Verify URL is reachable: `curl -H "Authorization: Bearer $STACKGEN_TOKEN" "$STACKGEN_MC_URL"`. |
| `mcp_server` mismatch | The `mcp_server` field in `[data_sources.jira]` must exactly match the `name` in `[[mcp.servers]]`. |
| No MCP resources listed | Not all MCP servers implement the resources protocol. Verify with `genie doctor`. |
| Keyword regex skips everything | Check patterns in `datasource_keyword_regex`. Invalid patterns are logged as warnings. |
| Linear comments missing | Verify `PM_API_TOKEN` is set and the Linear API supports `ListComments`. |
| SCM source not syncing | Verify `[scm]` provider + token are configured. SCM registers via `ExternalSources` at startup. |
| `scope.Get()` returns nil | Ensure scope was created with `NewScope("sourceName", values)` matching the connector's `Name()`. |
