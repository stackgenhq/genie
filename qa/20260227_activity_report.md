# Feature: Activity Report (Cron-Driven)

## Why

Genie was given a built-in “activity report” so it can periodically read its own audit log, summarize recent activities into a runbook-style report, write the report to disk, and store the summary in vector memory. This gives the agent a way to reflect on what it did and reuse that as runbook/skill for future runs.

## Problem

Without this feature, there was no automated way for Genie to turn recent tool calls, conversations, and other audit events into a persistent, searchable summary. Users and the agent could not rely on “what I did recently” being available as memory.

## Benefit

- **Scheduled reports**: A cron task with `action = "genie:report"` runs the report at a configurable schedule (e.g. daily at 09:00) without starting the full agent.
- **Stable paths**: Reports are written to `~/.genie/reports/<agent_name>/<YYYYMMDD>_<report_name>.md` so they are easy to find and script.
- **Vector memory**: The summary is upserted into the same vector store as runbooks, so `memory_search` and runbook search can retrieve past activity summaries for future answers.

---

## Test 1: Report file is written when cron runs with `genie:report`

### Arrange

- Genie is built (`make only-build` or `build/genie` available).
- A valid `.genie.toml` (or `genie.yaml`) exists with:
  - `agent_name` set (e.g. `agent_name = "my_agent"`).
  - At least one cron task with `action = "genie:report"` and a schedule that will run soon (for a quick test, use a short expression, e.g. `"* * * * *"` for every minute, or run Genie and wait until the next minute boundary).
- Optional: Some audit activity has occurred so the agent has written to `~/.genie/<agent_name>.<yyyy_mm_dd>.ndjson` (e.g. use the chat UI once so today’s audit file exists).
- Ensure `~/.genie` is writable (default).

Example config:

```toml
agent_name = "my_agent"

[[cron.tasks]]
name = "daily"
expression = "* * * * *"   # every minute for testing; use "0 9 * * *" for daily 09:00 in production
action = "genie:report"
```

### Act

1. Start Genie (e.g. `genie run` or `build/genie run`).
2. Wait for at least one cron tick after the next minute boundary (cron checks every minute). For `expression = "* * * * *"`, wait at least 1–2 minutes.
3. Stop Genie (Ctrl+C) or leave it running.

### Assert

1. The report file exists:  
   `~/.genie/reports/<agent_name>/<YYYYMMDD>_<report_name>.md`  
   e.g. `~/.genie/reports/my_agent/20260227_daily.md` (date is the day the cron ran; report name is the task `name`).  
   **Note:** The path has no time component — only the date. If the cron runs more than once per day (e.g. every 5 minutes), each run **appends** to the same file (with a `---` separator between runs). The file is only created new when it does not exist.
2. The file is non-empty and contains at least:
   - A heading such as `# Activity Report`
   - A **Period** line with a date range (UTC).
   - Either **Total events:** with a number, or **No activity in this window.**
3. Server logs show a line like:  
   `Activity report written` with `path` and `events` (or equivalent), with no error.

---

## Test 2: Report content reflects recent activity (when audit log has events)

### Arrange

- Same as Test 1, but ensure there is recent activity in the audit log:
  - Start Genie, open the chat UI (`docs/chat.html`), send at least one message and get a response (so `tool_call`, `conversation`, or similar events are written to today’s audit file).
- Configure a `genie:report` cron task (e.g. `name = "daily"`, `expression = "* * * * *"` for testing).

### Act

1. Wait for the next cron run (at least one full minute after the activity).
2. Open the report file for today:  
   `~/.genie/reports/<agent_name>/<YYYYMMDD>_daily.md`.

### Assert

1. The report contains a **By event type** section with at least one event type (e.g. `tool_call`, `conversation`) and a count.
2. If tool calls occurred, a **Tool usage** section lists tool names and counts.
3. No internal paths or implementation details are required; the content is human-readable markdown.

---

## Test 3: Activity report runs without vector store (file-only)

### Arrange

- Genie config has **no** `[vector_memory]` (or vector store is disabled/fails to init), so the app runs with no vector store.
- A `genie:report` cron task is configured (same as Test 1).

### Act

1. Start Genie.
2. Wait for one cron run of the report task.

### Assert

1. The report file is still created at `~/.genie/reports/<agent_name>/<YYYYMMDD>_<report_name>.md`.
2. Server logs may show a warning that vector store was skipped for the report; the run is still considered successful (no fatal error).
3. Genie continues to run; chat and other features work.

---

## Test 4: Report summary is searchable in memory (when vector store is enabled)

### Arrange

- Genie is configured with vector memory (e.g. `[vector_memory]` with `persistence_dir` and a valid embedding provider).
- `agent_name` is set; a `genie:report` cron task has run at least once so a report file exists and the summary was upserted.
- Chat UI is connected and working.

### Act

1. In the chat UI, ask a question that should be answered using the activity report summary, e.g.  
   “What did I do in my last activity report?” or “Summarize my recent activity.”
2. Rely on the agent’s use of `memory_search` / runbook search (no need to invoke tools manually).

### Assert

1. The agent’s reply is consistent with the content of the latest activity report (e.g. mentions recent events, tool usage, or “no activity” if the window was empty).
2. No requirement to assert exact tool calls; this is a blackbox check that the report summary is in memory and used when relevant.

---

## Troubleshooting

| Issue | What to check |
|-------|----------------|
| Report file not created | Confirm `action = "genie:report"` (exact string) and task `name` and `expression` are set. Check server logs for “activity report” or “genie:report” and any error. |
| Wrong path | Report path is `~/.genie/reports/<agent_name>/<YYYYMMDD>_<report_name>.md`. Agent name is sanitized (e.g. spaces → underscores). Date is the day the cron ran (YYYYMMDD). |
| Empty or “No activity” report | Audit log is read from `~/.genie/<agent_name>.<yyyy_mm_dd>.ndjson`. If no chat or tool use has occurred today (or in the lookback window), the report will be empty. |
| Cron not firing | Cron checks every minute. Ensure the expression matches the current time (e.g. `* * * * *` runs every minute). Check logs for “Cron scheduler” and “due tasks”. |
| Multiple sections in one file | The filename is per day only (`<YYYYMMDD>_<report_name>.md`). If the cron runs more than once per day, each run **appends** to the file (separated by `---`), so one file can contain multiple report sections. |
