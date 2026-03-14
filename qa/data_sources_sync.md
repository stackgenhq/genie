# Feature: Unified Data Sources Sync

## Why

This feature was developed to let Genie index content from external systems (Gmail, Google Drive, GitHub, GitLab, and eventually Slack, Linear, Calendar) into its vector memory so that `memory_search` can return relevant past emails, files, repo metadata, and PRs. Users can ask questions that rely on historical context without manually feeding that context.

## Problem

Previously, Genie’s long-term memory was limited to what was explicitly stored via tools or runbooks. There was no way to automatically sync Gmail labels, Drive folders, or chat channels into the vector store, so the agent could not search over that content.

## Benefit

- **Richer memory**: Emails and Drive files (and later Slack/Linear/GitHub/Calendar) are embedded and searchable.
- **Incremental sync**: Last sync time is persisted so only new or updated items are fetched on subsequent runs.
- **Scoped and filtered**: Config controls which sources, folders, labels, and keywords are indexed (e.g. only items matching search_keywords).

> **See also**: [20260313_mcp_datasources_and_scope_refactor.md](20260313_mcp_datasources_and_scope_refactor.md) for tests covering MCP-backed datasources (Jira, Confluence, ServiceNow), `datasource_keyword_regex` filtering, Linear issue comments, and the generic Scope refactor.

## Test 1: Data sources enabled and sync runs (Gmail, Drive, or GitHub/GitLab)

### Arrange

- Genie is configured with `[data_sources]` enabled, vector memory configured (e.g. persistence_dir and embedding provider), and at least one source enabled with valid scope (e.g. `[data_sources.gmail]` with `label_ids = ["INBOX"]` and Gmail credentials; `[data_sources.gdrive]` with `folder_ids` and Drive credentials; or `[data_sources.github]` / `[data_sources.gitlab]` with `repos = ["owner/repo"]` and `[scm]` provider set to the same provider).
- Working directory is writable so sync state can be persisted.

### Act

1. Start Genie (e.g. `genie run` or server start).
2. Wait for at least one sync interval (or until logs indicate a sync has run).
3. Optionally, in the chat UI, ask a question that would benefit from synced content (e.g. “What did we decide in recent emails about project X?”) and trigger `memory_search`.

### Assert

1. Server logs show at least one successful sync line (e.g. "data sources: synced", "source", "gmail", "gdrive", "github", or "gitlab", "items_listed", N, "chunks_upserted", M).
2. No error is shown that would indicate sync was skipped due to misconfiguration (e.g. missing credentials are logged as warnings; unsupported sources may log "sync not implemented for source, skipping").
3. If a question was asked, the reply is consistent with content that would have been synced (no requirement to assert exact text; blackbox check that memory is used).

## Test 2: Data sources disabled

### Arrange

- Genie config has `[data_sources]` with `enabled = false` (or section omitted).

### Act

1. Start Genie.

### Assert

1. No sync loop runs; logs do not show “data sources: synced” for the data-sources background job.
2. Server runs normally (chat, tools, etc. work).

## Test 3: Sync state persistence (incremental)

### Arrange

- Genie has run at least one successful sync for a source (e.g. gmail or gdrive).
- Working directory contains `datasource_sync_state.json`.

### Act

1. Restart Genie.
2. Trigger another sync (wait for interval or next run).

### Assert

1. Sync runs again; logs show sync for the same source.
2. If the connector supports incremental sync (ListItemsSince), only items updated after the last sync time are fetched (observable via lower item count or logs if available); otherwise full list is acceptable.

---

## Troubleshooting: “Genie has no data about me” / nothing ingested

If you asked Genie to learn from your data and it reports no stored information, or `memory_search` returns nothing, check the following.

### 1. **search_keywords is filtering everything out**

Only items whose **content or metadata** contains at least one of the configured keywords are indexed. If `search_keywords` is set to narrow terms (e.g. `["linear", "product", "stackgen"]`), most Gmail/Drive items will be skipped.

- **Fix**: To index all items from enabled sources, **remove or empty** `search_keywords` in `.genie.toml`:
  - `search_keywords = []` or omit the line.
- **Log**: Sync logs show `items` = number of items **listed** from the source; the number actually **upserted** can be lower due to keyword filtering. Check for `chunks_upserted` in the same log line to see how many chunks were written to the vector store.

### 2. **Vector memory not persisted**

If `[vector_memory]` has no `persistence_dir` (or it’s empty), the store is in-memory only. Restarting Genie clears it, so any previously synced data is lost.

- **Fix**: In `.genie.toml` add (or uncomment) under `[vector_memory]`:
  - `persistence_dir = ".genie/memory"` (or another writable path).
- Ensure the path is inside your working directory or a stable location so the same directory is used across runs.

### 3. **No items listed from the source**

Sync may report `items: 0` if the connector returns nothing (e.g. empty INBOX, no Drive folders, or API/scope issues).

- **Check**: Gmail scope (e.g. `label_ids = ["INBOX"]`), Drive `folder_ids`, and that OAuth/credentials are valid.
- **Logs**: Look for “data sources: list failed” or “skip gmail/gdrive” to see credential or connector errors.

### 4. **Sub-agent hit “max tool iterations”**

When Genie uses a sub-agent to run `memory_search`, it can hit the tool iteration limit and return no output even if the vector store has data.

- **Symptom**: Logs show “max tool iterations (5) exceeded” and “output_length”:0 for the memory sub-agent.
- **Mitigation**: Ensure the vector store actually has data (fix 1–3 above). If the store is empty, the sub-agent has nothing to return; fixing ingestion usually resolves this.

### Quick checklist

| Check | What to do |
|-------|------------|
| Index everything from sources | Set `search_keywords = []` or omit it |
| Persist memory across restarts | Set `[vector_memory]` `persistence_dir = ".genie/memory"` |
| Confirm something was indexed | Look for log line with `chunks_upserted` > 0 for that source |
| Gmail/Drive empty or errors | Check credentials, label_ids, folder_ids, and “list failed” / “skip” logs |
