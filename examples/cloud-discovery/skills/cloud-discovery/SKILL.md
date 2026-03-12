# Cloud Discovery & IaC Generation Runbook

Reference guide for discovering cloud resources, using StackGen `cloud2code`, querying cloud APIs, and managing IaC state safely.

---

## StackGen Discovery Tools (Primary Workflow)

The primary method for cloud discovery and IaC generation is using the dedicated StackGen MCP tools. These tools automatically scan cloud environments, translate findings into AppStacks, and generate Terraform or Helm configurations without relying on manual CLI commands.

### Discovery & AppStack Generation

| Tool Name | Use Case |
|---|---|
| `get_supported_cloud_resource_types` | Check which cloud resources are supported for discovery to avoid querying unsupported domains. |
| `create_appstack_from_brownfield_aws` | Directly discover existing AWS infrastructure and automatically convert it into an AppStack. Provide specific regions, tags, or ARNs to narrow scope. |
| `list_cloud_discoveries` | Retrieve and review historical or ongoing cloud discovery operations performed by StackGen. |
| `create_appstack_from_discovered_resources` | Translate a previously executed discovery operation's results into a StackGen AppStack. |

**Example Agent Workflow**:
1. Check what resources can be scanned (`get_supported_cloud_resource_types`).
2. Run AWS discovery on a specified region (`create_appstack_from_brownfield_aws`).
3. If looking for a past discovery, list them first (`list_cloud_discoveries`), then generate an AppStack from it (`create_appstack_from_discovered_resources`).

---

## Cloud Providers (Fallback / Read-Only CLI)

When the Native MCP Discovery tools need supplementing, or specific diagnostic information is requested, use the cloud provider CLIs.

**CRITICAL:** Only use read-only commands (`describe`, `get`, `list`).

### AWS Custom Queries

Make heavy use of `--query`, `--filter`, and `--output table` or `json` to reduce output size and noise before analysis.

```bash
# Network Topology
aws ec2 describe-vpcs --region $REGION --query 'Vpcs[*].{ID:VpcId,CIDR:CidrBlock,State:State}' --output table
aws ec2 describe-subnets --region $REGION --query 'Subnets[*].{ID:SubnetId,VPC:VpcId,CIDR:CidrBlock,AZ:AvailabilityZone}' --output table

# Compute Instances
aws ec2 describe-instances --region $REGION \
  --filters "Name=instance-state-name,Values=running" \
  --query 'Reservations[*].Instances[*].{ID:InstanceId,Type:InstanceType,VPC:VpcId,Subnet:SubnetId}' \
  --output table

# Security Group Rules
aws ec2 describe-security-groups --region $REGION \
  --query 'SecurityGroups[*].{ID:GroupId,Name:GroupName,VPC:VpcId,IpPermissions:IpPermissions}'
```

---

## Terraform Refactoring

When reorganizing auto-generated IaC from StackGen into more maintainable code structures, follow strict safety guidelines.

### Code Organization

- **Consolidate Granular Modules**: Group closely related infrastructure (e.g., a VPC, its Subnets, Route Tables, and Internet Gateways) into functional domains (e.g., a `network` module).
- **Simplifying Interfaces**: Abstract away raw cloud resource IDs from variables and outputs. Provide human-readable variables (like `environment` or `instance_count`).

### Safe State Migration

Use `moved` blocks to cleanly transition existing resources to new module paths without destroying and recreating them.

```hcl
# moves.tf
moved {
  from = module.stackgen_network.aws_vpc.this
  to   = module.core_network.aws_vpc.main
}

moved {
  from = module.stackgen_network.aws_subnet.this["subnet-0123456789abcdef0"]
  to   = module.core_network.aws_subnet.public[0]
}
```

### Validation & Safety Guardrails

**CRITICAL:** Refactoring should never result in infrastructure changes.

```bash
# Ensure formatting is clean
terraform fmt -recursive

# Validate the configuration syntactically
terraform validate

# Verify zero changes are planned (Add: 0, Change: 0, Destroy: 0)
terraform plan -detailed-exitcode
```

---

## Correlation Workflow

When investigating cloud infrastructure and validating its generated IaC:

1. **Discover via MCP Tools** — Route discovery tasks through `create_appstack_from_brownfield_aws` or build from established lists via `create_appstack_from_discovered_resources`.
2. **Supplemental Shell CLI** — If an imported architecture seems incomplete or unsupported (use `get_supported_cloud_resource_types` to verify), use `aws describe-*` or `gcloud compute * list` to verify raw cloud state.
3. **Analyze Generated Code** — Identify if the generated modules are too granular or hard to manage.
4. **Refactor and Map State** — Reorganize `.tf` files and inject `moved` blocks.
5. **Plan and Validate** — Always run `terraform plan` at the end to prove no resources are being destroyed.
