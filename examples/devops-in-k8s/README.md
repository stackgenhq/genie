# Genie DevOps Copilot – Kubernetes Deployment

Deploy the [Genie](https://github.com/stackgenhq/genie) DevOps Copilot to an EKS cluster with **AWS ReadOnly access** via IRSA and **secret management** via AWS Secrets Manager + External Secrets Operator.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  AWS                                                            │
│                                                                 │
│  ┌──────────────────────┐    ┌──────────────────────────────┐   │
│  │ IAM Role (IRSA)      │    │ Secrets Manager              │   │
│  │ • ReadOnlyAccess     │    │ platform/secrets/dev/genie   │   │
│  │                      │    │ ┌──────────────────────────┐ │   │
│  └──────────┬───────────┘    │ │ OPENAI_API_KEY           │ │   │
│             │                │ │ ANTHROPIC_API_KEY         │ │   │
│             │ assume-role    │ │ GEMINI_API_KEY            │ │   │
│             │                │ │ GITHUB_TOKEN              │ │   │
│  ┌──────────▼───────────┐    │ │ GRAFANA_URL               │ │   │
│  │ EKS: developer-eks   │    │ │ GRAFANA_API_KEY           │ │   │
│  │ namespace: genie     │    │ └──────────────────────────┘ │   │
│  │                      │    └──────────────┬───────────────┘   │
│  │ ┌──────────────────┐ │                   │                   │
│  │ │ ServiceAccount   │◄├───────────────────┘                   │
│  │ │ genie-sa (IRSA)  │ │   ExternalSecret sync                │
│  │ └────────┬─────────┘ │                                       │
│  │          │            │                                       │
│  │ ┌────────▼─────────┐ │                                       │
│  │ │ Deployment       │ │                                       │
│  │ │ genie-deployment │ │                                       │
│  │ │ ┌──────────────┐ │ │                                       │
│  │ │ │ genie.toml   │ │ │  (ConfigMap mount)                    │
│  │ │ │ env: secrets  │ │ │  (ExternalSecret → K8s Secret)       │
│  │ │ └──────────────┘ │ │                                       │
│  │ └────────┬─────────┘ │                                       │
│  │          │            │                                       │
│  │ ┌────────▼─────────┐ │                                       │
│  │ │ Service (ClusterIP)│                                       │
│  │ │ :80 → :9876      │ │                                       │
│  │ └────────┬─────────┘ │                                       │
│  │          │            │                                       │
│  │ ┌────────▼─────────┐ │                                       │
│  │ │ Ingress (nginx)  │ │                                       │
│  │ │ genie.dev.       │ │                                       │
│  │ │ stackgen.com     │ │                                       │
│  │ └─────────────────┘ │                                       │
│  └──────────────────────┘                                       │
└─────────────────────────────────────────────────────────────────┘
```

## Prerequisites

| Requirement | Details |
|---|---|
| **EKS Cluster** | With [OIDC provider](https://docs.aws.amazon.com/eks/latest/userguide/enable-iam-roles-for-service-accounts.html) configured |
| **External Secrets Operator** | [Installed](https://external-secrets.io/latest/introduction/getting-started/) on the cluster |
| **NGINX Ingress Controller** | Running on the cluster (NGINX Inc variant with `nginx.org/` annotations) |
| **AWS CLI / Profile** | Configured with admin access to the target account |
| **Terraform / OpenTofu** | ≥ 1.5 |

## Quick Start

### 1. Create the AWS Secrets Manager Secret

Store all required API keys in a single Secrets Manager secret as a JSON object:

```bash
aws secretsmanager create-secret \
  --name "platform/secrets/dev/genie" \
  --region us-west-2 \
  --secret-string '{
    "OPENAI_API_KEY": "sk-...",
    "ANTHROPIC_API_KEY": "sk-ant-...",
    "GEMINI_API_KEY": "AI...",
    "GITHUB_TOKEN": "ghp_...",
    "GRAFANA_URL": "https://grafana.example.com",
    "GRAFANA_API_KEY": "glsa_..."
  }'
```

> **Note:** The secret ARN will look like:
> `arn:aws:secretsmanager:us-west-2:339712749745:secret:platform/secrets/dev/genie-XXXXXX`
> You'll need the full ARN (including the random suffix) for the `aws_secrets_manager_arn` variable.

### 2. Configure Variables

Edit `dev.tfvars` with your values:

```hcl
aws_region              = "us-west-2"
eks_cluster_name        = "developer-eks"
namespace               = "genie"
create_namespace        = false                # set true if namespace doesn't exist
genie_image             = "ghcr.io/stackgenhq/genie:latest"
genie_replicas          = 1
ingress_host            = "genie.dev.stackgen.com"
agui_port               = 9876
aws_secrets_manager_arn = "arn:aws:secretsmanager:us-west-2:339712749745:secret:platform/secrets/dev/genie-XXXXXX"
```

### 3. Deploy

```bash
# Set your AWS profile
export AWS_PROFILE=339712749745_AdministratorAccess

# Initialize Terraform
terraform init

# Review the plan
terraform plan -var-file=dev.tfvars

# Apply
terraform apply -var-file=dev.tfvars
```

### 4. Verify

```bash
# Check the ExternalSecret synced successfully
kubectl get externalsecret -n genie

# Check the generated K8s secret
kubectl get secret genie-secrets -n genie

# Check deployment is running
kubectl get pods -n genie

# Check ingress
kubectl get ingress -n genie
```

## AWS Secrets Manager – Required Keys

The following keys **must** exist in the Secrets Manager secret as a JSON object:

| Key | Description | Required | Example |
|---|---|---|---|
| `OPENAI_API_KEY` | OpenAI API key for GPT models | **Yes** | `sk-proj-...` |
| `ANTHROPIC_API_KEY` | Anthropic API key for Claude models | **Yes** | `sk-ant-api03-...` |
| `GEMINI_API_KEY` | Google Gemini API key | **Yes** | `AIza...` |
| `GITHUB_TOKEN` | GitHub personal access token (for MCP + SCM) | **Yes** | `ghp_...` |
| `GRAFANA_URL` | Grafana instance URL (for MCP server) | Optional | `https://grafana.example.com` |
| `GRAFANA_API_KEY` | Grafana service account token | Optional | `glsa_...` |

### Updating Secrets

To update a secret value after initial creation:

```bash
aws secretsmanager put-secret-value \
  --secret-id "platform/secrets/dev/genie" \
  --region us-west-2 \
  --secret-string '{
    "OPENAI_API_KEY": "sk-NEW-KEY...",
    "ANTHROPIC_API_KEY": "sk-ant-...",
    "GEMINI_API_KEY": "AI...",
    "GITHUB_TOKEN": "ghp_...",
    "GRAFANA_URL": "https://grafana.example.com",
    "GRAFANA_API_KEY": "glsa_..."
  }'
```

The ExternalSecret refreshes every **15 minutes** by default. To force an immediate sync:

```bash
kubectl annotate externalsecret genie-secrets -n genie \
  force-sync=$(date +%s) --overwrite
```

## Terraform Resources Created

| Resource | Type | Purpose |
|---|---|---|
| `aws_iam_role.genie_readonly` | IAM Role | IRSA role with ReadOnlyAccess for AWS operations |
| `aws_iam_role_policy_attachment.readonly` | IAM Policy | Attaches AWS ReadOnlyAccess managed policy |
| `kubernetes_namespace.genie` | Namespace | Optional – creates the namespace if `create_namespace = true` |
| `kubernetes_manifest.secret_store` | SecretStore | Connects external-secrets operator to AWS Secrets Manager |
| `kubernetes_manifest.external_secret` | ExternalSecret | Syncs API keys from Secrets Manager into K8s Secret |
| `kubernetes_config_map.genie` | ConfigMap | Mounts `genie.toml` configuration into the pod |
| `kubernetes_service_account.genie` | ServiceAccount | Annotated with IRSA role ARN for AWS access |
| `kubernetes_deployment.genie` | Deployment | Runs the Genie container |
| `kubernetes_service.genie` | Service | ClusterIP service (port 80 → 9876) |
| `kubernetes_ingress_v1.genie` | Ingress | NGINX ingress at the configured hostname |

## Files

```
examples/devops-in-k8s/
├── main.tf           # Terraform configuration (source of truth)
├── dev.tfvars        # Variables for the developer-eks cluster
├── genie.toml        # Genie devops-copilot configuration
├── deployment.yaml   # Reference K8s manifests (non-Terraform alternative)
└── README.md         # This file
```

## Customization

### Adding More Secrets

To sync additional secrets (e.g., Datadog, PagerDuty), add entries to the `data` block in the `kubernetes_manifest.external_secret` resource in `main.tf`:

```hcl
{
  secretKey = "DD_API_KEY"
  remoteRef = {
    key      = var.aws_secrets_manager_arn
    property = "DD_API_KEY"
  }
},
```

Then add the corresponding key to your AWS Secrets Manager secret and uncomment the relevant MCP server section in `genie.toml`.

### Using a Different Ingress Host

Update the `ingress_host` variable in your `.tfvars` file. Make sure a DNS record (or wildcard) points to the NGINX ingress controller's load balancer:

```
k8s-ingressn-ingressn-efb5f6ecf7-a04f395ff369e7c5.elb.us-west-2.amazonaws.com
```

## Tear Down

```bash
terraform destroy -var-file=dev.tfvars
```

This will remove all Kubernetes and AWS resources. The AWS Secrets Manager secret is **not** managed by Terraform and will remain.
