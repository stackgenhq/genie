---
name: infrastructure-drift-detection
description: Detect infrastructure drift by comparing Terraform state against live resources and recommending remediation
---

# Infrastructure Drift Detection

Compare Terraform state against actual cloud resources to detect drift, identify unauthorized changes, and recommend remediation.

## When to Use This Skill

Use this skill when:
- Infrastructure drift needs to be detected before a deployment
- Compliance checks require verifying IaC matches reality
- Troubleshooting unexpected resource configurations
- Periodic drift audits are needed

## Prerequisites

- Terraform CLI installed (or OpenTofu)
- Terraform MCP server configured, OR shell access to run `terraform` commands
- Valid Terraform state (local or remote backend)

## Workflow

### Step 1: Initialize and Validate

```bash
# Navigate to Terraform directory
cd <terraform-directory>

# Ensure providers and modules are downloaded
terraform init -upgrade

# Validate configuration syntax
terraform validate
```

### Step 2: Plan to Detect Drift

```bash
# Run a plan in refresh-only mode to detect drift
# This compares the state file against actual cloud resources
terraform plan -refresh-only -detailed-exitcode 2>&1

# Exit codes:
# 0 = no drift
# 1 = error
# 2 = drift detected (changes found)

# For a full diff of what would change
terraform plan -no-color 2>&1 | head -200
```

### Step 3: Analyze Drift Details

```bash
# Show the current state
terraform show -json | jq '.values.root_module.resources[] | {type, name, address: .address}'

# List all resources in state
terraform state list

# Show specific resource details
terraform state show <resource_address>
```

### Step 4: Identify Root Cause

For each drifted resource, determine:
1. **What changed** — which attributes differ from the desired state
2. **Who changed it** — check cloud audit logs for the resource

```bash
# AWS CloudTrail — who modified the resource?
aws cloudtrail lookup-events --start-time $(date -u -d '7 days ago' +%s) \
  --lookup-attributes AttributeKey=ResourceName,AttributeValue=<resource-id> \
  --query "Events[].{Time:EventTime,User:Username,Action:EventName}" --output table

# GCP Audit Logs
gcloud logging read 'resource.type="<resource_type>" AND protoPayload.methodName=~"update|patch|delete"' --limit=20
```

### Step 5: Remediation Options

For each drifted resource, recommend one of:
1. **Apply** — run `terraform apply` to bring the resource back to desired state
2. **Import** — if the manual change was intentional, update the Terraform config and import
3. **Accept** — update the Terraform code to match the new reality

```bash
# Option 1: Bring resource back to Terraform-defined state
terraform apply -target=<resource_address> -auto-approve

# Option 2: Import a manually created resource
terraform import <resource_address> <resource-id>

# Option 3: Refresh state to accept current reality
terraform apply -refresh-only -auto-approve
```

> **⚠️ Always confirm with the user before applying changes**

## Output

Present a drift report with:
- **Drift Summary**: count of drifted resources by type
- **Drifted Resources**: table with resource address, attribute, expected vs actual value
- **Root Cause**: who made the change and when (from audit logs)
- **Remediation**: recommended action for each drifted resource
- **Prevention**: suggestions for preventing future drift (e.g., CI/CD guardrails)
