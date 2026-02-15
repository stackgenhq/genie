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
| Config file | Create `genie.toml` in the repo root with `[agui]` section, `port = 8080`, `cors_origins = ["*"]` |
| Chat UI | Open `docs/gh-pages/chat.html` in a browser |

---

## Test Suites

| Feature Area | File |
|---|---|
| Server & Connectivity | [20260214_server_connectivity.md](20260214_server_connectivity.md) |
| Chat Flows | [20260214_chat_flows.md](20260214_chat_flows.md) |
| Audit Logging | [20260214_audit_logging.md](20260214_audit_logging.md) |
| Summarizer | [20260214_summarizer.md](20260214_summarizer.md) |
| Human-in-the-Loop (HITL) | [20260214_hitl.md](20260214_hitl.md) |

---

## Known Gaps (Not Yet Implemented)

The following audit event types are defined in `pkg/audit/audit.go` but are **not yet emitted** by any code path:

| Event Type | Description | Status |
|---|---|---|
| `connection` | Logged when a client connects | ❌ Not wired |
| `disconnection` | Logged when a client disconnects | ❌ Not wired |
| `command` | Logged when a shell command is executed | ❌ Not wired |

These should be implemented in future work and added as acceptance tests once they are.
