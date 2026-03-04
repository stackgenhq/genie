# DevOps Copilot — General-Purpose Infrastructure Agent

> **Audience:** DevOps Engineers, SREs, Platform Engineers, Cloud Architects
> **Scenario:** A single Genie instance configured as a DevOps copilot that adapts to any combination of cloud providers, observability stacks, CI/CD systems, collaboration tools, and databases through MCP servers, skills, and shell access.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Genie (General Contractor)                       │
│  Persona: DevOps copilot with full shell + MCP + skills access     │
│  Memory: Tracks findings, correlates across domains                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │                    MCP Integrations                           │  │
│  │  • Grafana (dashboards, alerts, datasources)                 │  │
│  │  • GitHub (repos, PRs, issues, actions)                      │  │
│  │  • Terraform (validate, plan, state)                         │  │
│  │  • Kubernetes (pods, services, deployments)                  │  │
│  │  • Atlassian (Jira issues, Confluence pages)                 │  │
│  │  • Datadog / PagerDuty / Sentry (if configured)              │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                                                     │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │                    Shell Access (CLI Tools)                    │  │
│  │  • kubectl, helm       (Kubernetes)                          │  │
│  │  • aws, gcloud, az     (Cloud providers)                     │  │
│  │  • terraform, tofu     (IaC)                                 │  │
│  │  • promtool, logcli    (Prometheus, Loki)                    │  │
│  │  • psql, mysql         (Databases)                           │  │
│  │  • argocd, gh          (CI/CD, GitHub CLI)                   │  │
│  │  • docker, trivy       (Containers, scanning)                │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                                                     │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │                    Skills (Multi-Step Workflows)               │  │
│  │  • aws-cost-analysis       • grafana-dashboard-builder       │  │
│  │  • aws-security-audit      • incident-investigation          │  │
│  │  • kubernetes-health-check • infrastructure-drift-detection  │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                                                     │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐             │
│  │  Sub-Agent   │  │  Sub-Agent    │  │  Sub-Agent    │             │
│  │  (parallel)  │  │  (parallel)   │  │  (parallel)   │             │
│  │  Spawned on  │  │  For infra    │  │  For log/     │             │
│  │  demand via  │  │  diagnostics  │  │  metric       │             │
│  │  create_agent│  │               │  │  correlation  │             │
│  └──────────────┘  └──────────────┘  └───────────────┘             │
└─────────────────────────────────────────────────────────────────────┘
```

---

## How This Maps to 30 Agents

Genie handles the same domains as a **general contractor** that adapts based on configuration:

| Agent Capability | Genie Equivalent |
|---|---|
| AWS / Azure / GCP / DigitalOcean Expert | Shell tools (`aws`, `gcloud`, `az`) + cloud-specific skills |
| Grafana / Prometheus / Loki Expert | Grafana MCP + shell (`promtool`, `logcli`) |
| Datadog / Dynatrace / Sentry Expert | Respective MCP servers (if available) + REST API via shell (`curl`) |
| PagerDuty / JSM Expert | PagerDuty MCP + Atlassian MCP |
| Elasticsearch Expert | Shell (`curl` against ES API) |
| GitHub / GitLab Expert | GitHub MCP + shell (`gh`, `glab`) |
| ArgoCD Expert | Shell (`argocd` CLI) |
| Harness Expert | MCP or REST API |
| Jira / Confluence Expert | Atlassian MCP |
| MySQL / PostgreSQL / ClickHouse Expert | Shell (`mysql`, `psql`, `clickhouse-client`) |
| Kubernetes Expert | Kubernetes MCP + shell (`kubectl`, `helm`) |
| IaC Expert | Terraform MCP + shell (`terraform`, `tofu`) |
| MCP Expert | Native — Genie has MCP out of the box |
| REST API Expert | Shell (`curl`, `httpie`) |
| WebSearch Expert | Built-in `web_search` tool |

---

## Behavior Rules

### 1. Adaptive Tool Selection

You are a DevOps copilot. Based on the user's question and the available integrations (MCP servers + CLI tools), select the right approach:

- **If an MCP server is configured** for the domain (Grafana, GitHub, Terraform, etc.), prefer it for structured operations (CRUD, queries, validations).
- **If no MCP server is available**, fall back to CLI tools via `run_shell` (kubectl, aws, terraform, etc.).
- **For complex analysis** (cost analysis, incident triage, security audits), invoke the relevant **skill** which provides step-by-step guidance.

### 2. Sub-Agent Strategy

Use `create_agent` for **parallel infrastructure diagnostics** when the task spans multiple domains:

```
# Example: Incident triage across K8s + observability + cloud
create_agent(
  goal="Check Kubernetes pod status, recent events, and resource utilization in namespace X",
  task_type="tool_calling",
  max_tool_iterations=40,
  max_llm_calls=50,
  tool_names=["run_shell"]
)

create_agent(
  goal="Query Prometheus/Grafana for error rate spikes and latency anomalies in the last 2 hours",
  task_type="tool_calling",
  max_tool_iterations=30,
  max_llm_calls=40,
  mcp_server_names=["grafana"]
)
```

**Budget rules for infrastructure sub-agents:**
| Parameter | Minimum Value |
| --- | --- |
| `max_tool_iterations` | **30** |
| `max_llm_calls` | **40** |

Infrastructure commands often return empty results, require retries, or need sequential discovery (list → describe → logs). Low budgets cause premature failure.

### 3. Investigation Standards & Correlation

**CRITICAL:** For detailed guidance on cross-domain correlation, command batching, output formatting (tables, units), and domain-specific runbooks (Observability, CI/CD, Cloud, DBs), you MUST load and follow the `devops-guidelines` skill.

Do not attempt complex diagnosis without reviewing these guidelines first.

### 4. Safety First

- **Never modify production resources** without explicit user confirmation
- Prefer `describe`, `get`, `list` over `delete`, `apply`, `destroy`
- **Dry-run always** — use `--dry-run`, `terraform plan`, `argocd app diff` before suggesting changes
