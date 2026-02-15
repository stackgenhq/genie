# Genie Roadmap (Inspired by OpenClaw)

This roadmap outlines the plan to integrate key features from [OpenClaw](https://github.com/openclaw/openclaw) into `genie`, transforming it from a powerful CLI into an enterprise-ready Infrastructure-as-Code (IaC) platform.

## Core Philosophy: "Enterprise Ready"
All additions must adhere to strict enterprise standards:
- **Security First**: mTLS, RBAC, and Audit Logging are foundational, not afterthoughts.
- **Observability**: Structured logging (OpenTelemetry), metrics, and distributed tracing.
- **Scalability**: Stateless services where possible, with persistent state offloaded to reliable backends (Postgres/S3).
- **Compliance**: Policy-as-Code (OPA/Sentinel) integration at the agent level.

---

## 🏗️ 1 Day Plan: The Secure Foundation
**Goal**: Harden the existing "remote" capability (`TCPListener`) to be production-safe.

*   **Secure the Port**:
    *   [ ] Add a simple **API Token Auth** handshake as an alternative for simpler clients (with rotation support).
*   **Structured Observability**:
    *   [ ] Standardize all TUI events to follow the **AG-UI protocol** event types (`pkg/tui/events.go`), with **CloudEvents envelope** for external system integration.
*   **Health Checks**:
    *   [ ] Add a `/healthz` equivalent signal (or specific JSON message) to the TCP listener for liveness probes.

## 🚀 1 Week Plan: The Headless Gateway
**Goal**: Decouple the Agent Runtime from the TUI, enabling "Headless Mode".

*   **The Gateway Service**:
    *   [ ] Refactor `cmd/genie` to support a `serve` or `daemon` command.
    *   [ ] Create a `Gateway` struct in `pkg/gateway` that wraps the `expert` and `reactree` packages.
    *   [ ] Support **Bi-directional Communication**: Allow the agent to ask questions back to the remote client (e.g., "Confirm deletion of resource X?") via the TCP/WebSocket stream.
*   **Generic Communication Layer (`pkg/messenger`)**:
    *   [ ] Implement **Outbound Notifications**: `github.com/containrrr/shoutrrr` for fire-and-forget alerts.

> [!NOTE]
> This is single-tenancy for now — one Genie instance per user/team.

## 🌙 1 Month Plan: Policy & Compliance
**Goal**: Add enterprise governance guardrails.

*   **Policy Sentinel**:
    *   [ ] A dedicated "Compliance Agent" that reviews every generated plan *before* it's presented to the user.
    *   [ ] Integrate with OPA/Sentinel for corporate policy checks (e.g., "No public S3 buckets").
*   **Proactive Monitoring**:
    *   [ ] Drift detection: compare generated IaC state with live infrastructure.
    *   [ ] Schedule periodic re-analysis via cron-like skills.

## 🔭 1 Year Plan: The Enterprise Platform
**Goal**: A central nervous system for Infrastructure Engineering.

*   **Genie Platform (Headless + UI)**:
    *   [ ] **Web Dashboard (Live Canvas)**: A React-based frontend visualizing the live infrastructure graph, current deployments, and agent thoughts (replacing the TUI for the team view).
    *   [ ] **Policy Sentinel**: A dedicated "Compliance Agent" that reviews every plan *before* it's presented to the user, checking against corporate policies (e.g., "No public S3 buckets").
*   **Ecosystem Integration**:
    *   [ ] **GitHub App**: "Genie" as a bot that comments on PRs, detects drift in code-reviews, and can "fix" IaC PRs automatically.
    *   [ ] **JIRA/ServiceNow**: Auto-create tickets for infrastructure changes and link them to the generated PRs.
*   **Self-Improving Cycle**:

## 🛠️ Tool Ecosystem & Personas
**Goal**: tailoring the agent's capabilities to specific user needs while sharing a common core.

### Persona Classification
| Persona | Focus | Key Tools |
| :--- | :--- | :--- |
| **Common** | General Productivity | Web Search, Weather, Calculator, Email, Calendar |
| **Developers** | Code & logic | SCM (GitHub/GitLab), IDE Utils, Docs Search, Jira/Linear |
| **DevOps** | Infra & Operations | Terraform (TFTools), Kubernetes, Cloud Providers, PagerDuty |
| **SecOps** | Security & Compliance | Trivy, Snyk, Audit Logs, Policy Checkers |

### Integration Roadmap

#### 1. Unified SCM Support (Developers/DevOps)
*   **Strategy**: Wrap `github.com/appcd-dev/go-scm` to provide a normalized interface for all Git providers.
*   **Capabilities**:
    *   [ ] `scm_list_repos`: List accessible repositories.
    *   [ ] `scm_get_pr`: Read PR details and comments (critical for "Genie as a bot").
    *   [ ] `scm_create_pr`: Open PRs for generated code/IaC.
    *   [ ] `scm_review_pr`: Post comments/reviews on PRs.

#### 2. Project Management (Developers/Product)
*   **Strategy**: Abstract "Issue Tracking" into a common interface (JIRA, Linear, Asana).
*   **Capabilities**:
    *   [ ] `pm_get_issue`: Read ticket details/status.
    *   [ ] `pm_create_issue`: Report bugs or tasks found during analysis.
    *   [ ] `pm_assign_issue`: Route tasks to humans.

#### 3. Communication & Notifications (Common)
*   **Strategy**: Standard Email client support.
*   **Capabilities**:
    *   [ ] `email_send`: Send reports, summaries, or urgent alerts.
    *   [ ] `email_read`: (Optional) Trigger agent actions via email instructions.

