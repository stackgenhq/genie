# Ticket-to-PR: Autonomous Issue Resolution

> **Audience:** Engineering Teams & Tech Leads
> **Scenario:** A developer says *"Genie, work on LINEAR-423"* in Slack.
> Genie reads the ticket, writes the code, runs tests, creates a PR,
> updates the ticket status, and posts the PR link back to Slack.

---

## The Problem

Your backlog is full. Tickets pile up. Context-switching between your issue tracker,
IDE, terminal, and PR interface kills velocity. What if you could assign a ticket
to an AI that understands your codebase, follows your coding standards, and delivers
a tested PR — with human approval at every write step?

---

## Architecture

```
                          ┌───────────────────────────────────────┐
                          │           Slack / AG-UI               │
                          │     "Genie, work on LINEAR-423"       │
                          └──────────────┬────────────────────────┘
                                         │
                          ┌──────────────▼────────────────────────┐
                          │          CodeOwner (Orchestrator)     │
                          │  • Classify → route to ReAcTree       │
                          │  • Load Agents.md agent persona       │
                          │  • Retrieve relevant memories         │
                          └──────────────┬────────────────────────┘
                                         │
                ┌────────────────────────┤────────────────────────┐
                │                        │                        │
    ┌───────────▼──────────┐ ┌──────────▼───────────┐ ┌──────────▼───────────┐
    │  Stage 1: Understand │ │  Stage 2: Implement  │ │  Stage 3: Deliver    │
    │  ─────────────────── │ │  ──────────────────  │ │  ─────────────────   │
    │  • Read ticket       │ │  • Write code        │ │  • Run tests         │
    │  • Search codebase   │ │  • Follow standards  │ │  • Run linter        │
    │  • Check runbooks    │ │  • Add tests         │ │  • Create PR         │
    │  • Plan approach     │ │  • HITL on writes ⚠️ │ │  • Update ticket     │
    │                      │ │                      │ │  • Notify Slack ✅   │
    │  Model: gemini-pro   │ │  Model: gemini-flash │ │  Model: gemini-flash │
    └──────────────────────┘ └──────────────────────┘ └──────────────────────┘
```

---

## Prompts — What to Say

### The One-Liner

```
Work on LINEAR-423
```

Genie reads the ticket, understands the requirements, finds relevant code,
implements the changes, and delivers a PR.

### With Context

```
Work on LINEAR-423. The codebase uses Go with Ginkgo/Gomega tests.
Follow the patterns in pkg/service/ for the service layer
and pkg/handler/ for HTTP handlers.
```

### Multi-Ticket Sprint

```
Work on these tickets in order:
1. LINEAR-423 — Add user creation endpoint
2. LINEAR-424 — Add email validation
3. LINEAR-425 — Add rate limiting to auth endpoints

Create a separate PR for each ticket. Update each ticket status
to "In Review" when the PR is ready.
```

### Bug Fix

```
Fix the bug in LINEAR-512. The error says "nil pointer dereference
in pkg/service/order.go:123". Read the ticket for reproduction steps,
write a failing test first, then fix it.
```

### Security Review + Fix

```
LINEAR-601 is a security vulnerability report. Read the ticket,
search the codebase for the affected pattern, fix all instances,
and add regression tests. Create the PR with the "security" label.
```

### With Runbook

```
Work on LINEAR-789 using our deployment runbook.
Check .genie/runbooks/deployment-checklist.md for required steps.
```

---

## Interaction Flow (Slack)

### Step 1 — Trigger from Slack

```
@Genie work on LINEAR-423
```

Genie responds in-thread:

```
🎯 Working on LINEAR-423: "Add CreateUser API endpoint"

📖 Reading ticket details...
   Priority: High | Assignee: unassigned | Labels: backend, api
   
   Requirements:
   • POST /api/v1/users endpoint
   • Email uniqueness validation
   • Return 201 with user object on success
   
🔍 Searching codebase for patterns...
   Found: pkg/handler/order.go (similar endpoint pattern)
   Found: pkg/service/order.go (service layer pattern)
   Found: AGENTS.md (coding standards loaded)

📝 Plan:
   1. Create UserService with CreateUser method
   2. Create HTTP handler with input validation
   3. Add Ginkgo unit tests with counterfeiter fakes
   4. Run tests + linter
   5. Create PR and update ticket
```

### Step 2 — HITL Approval (Slack Block Kit)

When Genie tries to write a file, Slack shows a rich approval card:

```
⚠️ Approval Required — write_file
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

💡 Why: Creating service layer for CreateUser endpoint per LINEAR-423

📋 Arguments:
┌─────────────────────────────────────────────┐
│ { "path": "pkg/service/user.go",            │
│   "content": "package service\n\nimport..." }│
└─────────────────────────────────────────────┘

  ┌──────────┐  ┌──────────┐  ┌──────────┐
  │ ✅ Approve│  │ 🔄 Revisit│  │ ❌ Reject │
  └──────────┘  └──────────┘  └──────────┘

  Reply Yes to approve, No to reject, or type feedback to revisit.
```

### Step 3 — Delivery

```
✅ LINEAR-423 Complete!

📦 PR #42: feat: add CreateUser endpoint
   https://github.com/myorg/myrepo/pull/42
   
   Changes:
   • pkg/service/user.go — CreateUser service (2-param pattern ✅)
   • pkg/handler/user.go — HTTP handler with validation
   • pkg/service/user_test.go — 4 test cases, all passing ✅
   • golangci-lint: 0 errors ✅
   
📋 LINEAR-423 status updated: In Progress → In Review
```

---

## Key Capabilities Demonstrated

| Capability | How It's Used |
|-----------|---------------|
| **Multi-model routing** | Planning with gemini-pro, tool calling with gemini-flash |
| **PM integration** | Read ticket → implement → update status → done |
| **SCM integration** | Create branch, commit, push, open PR with description |
| **Slack integration** | Trigger via @mention, stream progress, HITL approval cards |
| **HITL approval** | Write operations require human approval in Slack |
| **Coding standards** | AGENTS.md loaded automatically, enforced in all code changes |
| **Sub-agent delegation** | Write-heavy tasks delegated to cheaper models |
| **Vector memory** | Remembers patterns from previous sessions |
| **PII redaction** | Secrets scrubbed before reaching memory or audit logs |
| **Runbook search** | Follows team playbooks when available |

---

## Sample Prompts by Role

### For Engineering Managers

```
Summarize the last 5 completed tickets and create a weekly status
report. Post it to #engineering-updates.
```

### For SREs

```
LINEAR-890 reports high latency in the payment service.
Check CloudWatch metrics, review recent deployments,
and create a fix if the root cause is in our code.
```

### For Security Engineers

```
Audit all endpoints in pkg/handler/ for proper authentication.
Create tickets for any endpoints missing auth middleware.
```

---

## Getting Started

```bash
# 1. Clone and navigate to the example
cd examples/ticket-to-pr

# 2. Set environment variables
export GEMINI_API_KEY="..."
export GITHUB_TOKEN="..."
export PM_API_TOKEN="..."
export SLACK_APP_TOKEN="xapp-..."
export SLACK_BOT_TOKEN="xoxb-..."

# 3. Start local embeddings (optional but recommended)
ollama serve &
ollama pull nomic-embed-text

# 4. Run Genie
genie grant

# 5. In Slack, mention @Genie:
#    "Work on LINEAR-423"
```
