# SecOps: Infrastructure-as-Code Security Audit

> **Audience:** Security Operations Engineers  
> **Scenario:** A SecOps engineer needs to audit Terraform configurations across multiple repositories for compliance violations, exposed secrets, and misconfigured IAM policies before a quarterly SOC 2 review.

---

## The Problem

Your organization has 12 microservices, each with its own `infrastructure/` directory containing Terraform modules. A SOC 2 audit is in two weeks. You need to:

1. Scan all Terraform files for hardcoded secrets and overly permissive IAM policies
2. Verify encryption-at-rest is enabled on every storage resource (S3, RDS, EBS)
3. Ensure network security groups don't allow unrestricted ingress (`0.0.0.0/0`)
4. Generate a compliance report with remediation guidance

Doing this manually across 12 repos would take days. With Genie's multi-agent system, MCP tools, and skills, it takes minutes.

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    CodeOwner (Orchestrator)              │
│  Persona: SecOps auditor with full shell + file access  │
│  Memory: Tracks findings across all repos               │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐  │
│  │  Sub-Agent 1 │  │  Sub-Agent 2  │  │  Sub-Agent 3  │  │
│  │  IAM Audit   │  │  Encryption   │  │  Network SGs  │  │
│  │  (parallel)  │  │  (parallel)   │  │  (parallel)   │  │
│  └──────┬──────┘  └──────┬───────┘  └──────┬────────┘  │
│         │                │                  │           │
│  ┌──────┴────────────────┴──────────────────┴────────┐  │
│  │              Shared Tool Registry                  │  │
│  │  • File tools (read, search, list)                │  │
│  │  • Shell tool (tfsec, checkov, grep)              │  │
│  │  • MCP: terraform-mcp-server (validate, plan)     │  │
│  │  • Skill: terraform-validate                      │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

---

## Configuration

### `genie.toml`

```toml
skills_roots = ["./skills"]

[mcp]
[[mcp.servers]]
name = "terraform"
transport = "stdio"
command = "npx"
args = ["-y", "@anthropic/terraform-mcp-server"]

[[mcp.servers]]
name = "github"
transport = "stdio"
command = "npx"
args = ["-y", "@anthropic/github-mcp-server"]
env = { GITHUB_TOKEN = "${GITHUB_TOKEN}" }
include_tools = ["search_repositories", "get_file_contents", "list_commits"]
```

---

## Interaction Flow

### Step 1 — User asks for a full audit

```
You: Audit all Terraform code in ./services/*/infrastructure/ for SOC 2 compliance.
     Check for: hardcoded secrets, permissive IAM, missing encryption, open security groups.
     Generate a compliance report.
```

### Step 2 — CodeOwner plans the work

The orchestrator reads the directory structure and creates a plan:

```
Genie: I found 12 service directories with Terraform configurations.
       I'll run parallel audits across three categories:

       1. IAM & Secrets audit (all services)
       2. Encryption-at-rest verification (S3, RDS, EBS resources)  
       3. Network security group analysis

       Spawning 3 sub-agents for parallel execution...
```

### Step 3 — Multi-agent execution

The CodeOwner delegates using `create_agent`:

```
# Sub-Agent 1: IAM & Secrets (runs in parallel)
create_agent(
  goal="Scan all .tf files in ./services/*/infrastructure/ for:
        1. Hardcoded AWS access keys or secrets (regex: AKIA[0-9A-Z]{16})
        2. IAM policies with 'Effect: Allow' on 'Resource: *'
        3. IAM roles without condition blocks
        Report findings as JSON with file, line, severity, description.",
  task_type="tool_calling",
  tool_names=["search_content", "read_file", "list_file", "execute_command"]
)

# Sub-Agent 2: Encryption audit (runs in parallel)
create_agent(
  goal="Check all .tf files in ./services/*/infrastructure/ for:
        1. aws_s3_bucket resources missing server_side_encryption_configuration
        2. aws_db_instance resources with storage_encrypted = false or missing
        3. aws_ebs_volume resources without encryption = true
        Report each finding with file path, resource name, and remediation.",
  task_type="tool_calling",
  tool_names=["search_content", "read_file", "list_file"]
)

# Sub-Agent 3: Network security (runs in parallel)  
create_agent(
  goal="Analyze all aws_security_group and aws_security_group_rule resources in
        ./services/*/infrastructure/ for:
        1. Ingress rules allowing 0.0.0.0/0 (except port 443)
        2. Egress rules allowing all traffic
        3. Missing description fields on rules
        Run 'tfsec ./services --format json' for additional findings.
        Merge results into a single report.",
  task_type="tool_calling",
  tool_names=["search_content", "read_file", "execute_command"]
)
```

### Step 4 — MCP terraform server validates

After sub-agents report findings, the orchestrator uses the Terraform MCP server to validate proposed fixes:

```
# CodeOwner uses MCP terraform tools to validate
terraform_validate(directory="./services/payment-api/infrastructure/")
terraform_plan(directory="./services/payment-api/infrastructure/", var_file="staging.tfvars")
```

### Step 5 — Compliance report generation

The orchestrator synthesizes all findings:

```
Genie: ## SOC 2 Compliance Audit Report

       ### Summary
       | Category           | Critical | High | Medium | Passed |
       |--------------------|----------|------|--------|--------|
       | Secrets & IAM      | 2        | 5    | 3      | 47     |
       | Encryption at Rest | 1        | 3    | 0      | 28     |
       | Network Security   | 0        | 4    | 6      | 19     |

       ### Critical Findings
       1. **Hardcoded AWS key** in `services/legacy-api/infrastructure/provider.tf:7`
          → Remediation: Move to AWS Secrets Manager, reference via data source
       2. **IAM policy with Resource: * and Action: *** in `services/admin/infrastructure/iam.tf:23`
          → Remediation: Scope to specific ARNs per least privilege

       ### Auto-Generated Fix PRs
       I can create fix branches for each finding. Shall I proceed?
```

---

## Key Concepts Demonstrated

| Concept | How It's Used |
|---------|---------------|
| **CodeOwner orchestrator** | Plans the audit, delegates sub-tasks, synthesizes the report |
| **Multi-agent (ReAcTree)** | 3 parallel sub-agents for IAM, encryption, and network audits |
| **MCP tools** | Terraform MCP server for `validate` and `plan` on proposed fixes |
| **Skills** | `terraform-validate` skill for structured validation checks |
| **Working memory** | Findings accumulate across sub-agent returns, no duplicate scanning |
| **Shell access** | Runs `tfsec`, `checkov`, `grep` for static analysis |

---

## Why Multi-Agent?

**Without multi-agent:** The orchestrator would scan 12 repos sequentially across 3 concern areas = 36 sequential scans. Estimated: ~15 minutes.

**With multi-agent:** 3 sub-agents run in parallel, each scanning all 12 repos for their specific concern. The orchestrator only does planning + synthesis. Estimated: ~3 minutes.

The sub-agents use `task_type="tool_calling"` which routes to faster, cheaper models — perfect for the repetitive file-scanning work that doesn't require sophisticated reasoning.
