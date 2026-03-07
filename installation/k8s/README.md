# Genie – Kubernetes Installation

Deploy [Genie](https://github.com/stackgenhq/genie) to an EKS cluster with Terraform.

## Overview

This directory contains a Terraform-based deployment for Genie on Kubernetes (EKS). It provisions:

- **IAM Role** with AWS `ReadOnlyAccess` via [IRSA](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html) (optional)
- **ExternalSecret** to sync API keys from AWS Secrets Manager into Kubernetes (optional)
- **ConfigMap** with `genie.toml` loaded from a separate file
- **Deployment**, **Service**, and **Ingress** for the Genie container

```
installation/k8s/
├── main.tf                   # Terraform configuration (all resources)
├── genie.toml                # Genie config template (edit this)
├── terraform.tfvars.example  # Example variables (copy → terraform.tfvars)
├── deployment.yaml           # Reference YAML (non-Terraform alternative)
└── README.md                 # This file
```

## Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│  AWS Account                                                       │
│                                                                    │
│  ┌─────────────────────┐      ┌────────────────────────────────┐  │
│  │ IAM Role (IRSA)     │      │ Secrets Manager                │  │
│  │ ReadOnlyAccess      │      │                                │  │
│  └──────────┬──────────┘      │  { "OPENAI_API_KEY": "sk-...", │  │
│             │                 │    "ANTHROPIC_API_KEY": "...",  │  │
│             │                 │    "GEMINI_API_KEY": "...",     │  │
│             │ assume-role     │    "GITHUB_TOKEN": "ghp_..." } │  │
│             │                 └───────────────┬────────────────┘  │
│  ┌──────────▼──────────────────────────────────▼───────────────┐  │
│  │  EKS Cluster                                                │  │
│  │                                                             │  │
│  │  ┌───────────────┐    ┌──────────────┐   ┌──────────────┐  │  │
│  │  │ ServiceAccount│───▸│ SecretStore  │──▸│ExternalSecret│  │  │
│  │  │ genie-sa      │    │ (AWS SM)     │   │ → K8s Secret │  │  │
│  │  └───────┬───────┘    └──────────────┘   │ genie-secrets│  │  │
│  │          │                               └──────┬───────┘  │  │
│  │  ┌───────▼───────────────────────────────────────▼──────┐  │  │
│  │  │ Deployment                                           │  │  │
│  │  │  ┌─────────────────────────────────────────────────┐ │  │  │
│  │  │  │ Pod                                             │ │  │  │
│  │  │  │  • /app/genie.toml ← ConfigMap                  │ │  │  │
│  │  │  │  • env vars        ← genie-secrets              │ │  │  │
│  │  │  │  • AWS creds       ← IRSA (auto-injected)       │ │  │  │
│  │  │  └─────────────────────────────────────────────────┘ │  │  │
│  │  └──────────────────────────┬───────────────────────────┘  │  │
│  │                             │                               │  │
│  │  ┌──────────────────────────▼────────────────────────────┐ │  │
│  │  │ Service (ClusterIP :80) → Ingress (nginx)             │ │  │
│  │  └───────────────────────────────────────────────────────┘ │  │
│  └─────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────┘
```

## Prerequisites

| Requirement | Details |
|---|---|
| **EKS Cluster** | With [OIDC provider](https://docs.aws.amazon.com/eks/latest/userguide/enable-iam-roles-for-service-accounts.html) enabled |
| **External Secrets Operator** | [Installed](https://external-secrets.io/latest/introduction/getting-started/) on the cluster |
| **Ingress Controller** | NGINX or ALB ingress controller running |
| **Terraform / OpenTofu** | ≥ 1.5 |
| **AWS CLI** | Configured with appropriate credentials |

## Quick Start

### 1. Create the AWS Secrets Manager Secret

Store your API keys as a JSON object in Secrets Manager:

```bash
aws secretsmanager create-secret \
  --name "genie/api-keys" \
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

Note the full ARN returned (including the random suffix like `-AbCdEf`).

### 2. Configure

```bash
# Copy the example variables file
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars`:

```hcl
aws_region              = "us-west-2"
eks_cluster_name        = "my-cluster"
namespace               = "genie"
create_namespace        = true
aws_secrets_manager_arn = "arn:aws:secretsmanager:us-west-2:123456789012:secret:genie/api-keys-AbCdEf"
ingress_host            = "genie.example.com"
```

### 3. Customize `genie.toml`

Edit `genie.toml` to enable/disable MCP servers and adjust model providers for your use case. The file is loaded by Terraform and mounted as a ConfigMap.

### 4. Deploy

```bash
terraform init
terraform plan  -var-file=terraform.tfvars
terraform apply -var-file=terraform.tfvars
```

### 5. Verify

```bash
# Check ExternalSecret synced
kubectl get externalsecret -n genie
# Expected: STATUS = SecretSynced, READY = True

# Check deployment
kubectl get pods -n genie
# Expected: 1/1 Running

# Check ingress
kubectl get ingress -n genie
```

## Configuration Reference

### Terraform Variables

| Variable | Type | Default | Description |
|---|---|---|---|
| `aws_region` | string | `us-east-1` | AWS region |
| `eks_cluster_name` | string | _(required)_ | EKS cluster name |
| `namespace` | string | `default` | K8s namespace |
| `create_namespace` | bool | `false` | Create the namespace |
| `genie_image` | string | `ghcr.io/stackgenhq/genie:latest` | Container image |
| `genie_replicas` | number | `1` | Replica count |
| `ingress_host` | string | `genie.local` | Ingress hostname |
| `ingress_class_name` | string | `nginx` | IngressClass |
| `ingress_annotations` | map(string) | `{}` | Ingress annotations |
| `container_port` | number | `9876` | Container port |
| `genie_config_file` | string | `genie.toml` | Path to config file |
| `aws_secrets_manager_arn` | string | _(required if ESO enabled)_ | Secrets Manager ARN |
| `enable_external_secrets` | bool | `true` | Enable ExternalSecret |
| `enable_irsa` | bool | `true` | Enable IAM IRSA role |
| `external_secret_keys` | map(string) | _(see below)_ | Secret keys to sync |

### Required Secret Keys

These keys must exist in the Secrets Manager secret JSON:

| Key | Description | Required |
|---|---|---|
| `OPENAI_API_KEY` | OpenAI API key for GPT models | **Yes** |
| `ANTHROPIC_API_KEY` | Anthropic API key for Claude models | **Yes** |
| `GEMINI_API_KEY` | Google Gemini API key | **Yes** |
| `GITHUB_TOKEN` | GitHub PAT for MCP + SCM | **Yes** |
| `GRAFANA_URL` | Grafana instance URL | Optional |
| `GRAFANA_API_KEY` | Grafana service account token | Optional |

### Adding More Secrets

Add entries to `external_secret_keys` in your `terraform.tfvars`:

```hcl
external_secret_keys = {
  OPENAI_API_KEY    = "OPENAI_API_KEY"
  ANTHROPIC_API_KEY = "ANTHROPIC_API_KEY"
  GEMINI_API_KEY    = "GEMINI_API_KEY"
  GITHUB_TOKEN      = "GITHUB_TOKEN"
  GRAFANA_URL       = "GRAFANA_URL"
  GRAFANA_API_KEY   = "GRAFANA_API_KEY"
  DD_API_KEY        = "DD_API_KEY"        # Add new keys here
  DD_APP_KEY        = "DD_APP_KEY"
}
```

Then add matching keys to your Secrets Manager secret and uncomment the relevant MCP server section in `genie.toml`.

### Updating Secrets

```bash
aws secretsmanager put-secret-value \
  --secret-id "genie/api-keys" \
  --region us-west-2 \
  --secret-string '{ ... updated JSON ... }'
```

Force immediate sync (instead of waiting for the 15m refresh):

```bash
kubectl annotate externalsecret genie-secrets -n genie \
  force-sync=$(date +%s) --overwrite
```

## Feature Flags

### Deploying Without IRSA (no AWS access)

```hcl
enable_irsa = false
```

The ServiceAccount will be created without the IRSA annotation. The pod won't have AWS credentials.

### Deploying Without External Secrets (manual K8s Secret)

```hcl
enable_external_secrets = false
```

Create the K8s secret manually:

```bash
kubectl create secret generic genie-secrets -n genie \
  --from-literal=OPENAI_API_KEY=sk-... \
  --from-literal=ANTHROPIC_API_KEY=sk-ant-... \
  --from-literal=GEMINI_API_KEY=AI... \
  --from-literal=GITHUB_TOKEN=ghp_...
```

### Minimal Deployment (no AWS, no secrets)

```hcl
enable_irsa             = false
enable_external_secrets = false
```

## Tear Down

```bash
terraform destroy -var-file=terraform.tfvars
```

> **Note:** The AWS Secrets Manager secret is **not** managed by Terraform and will remain after destroy.

## Non-Terraform Alternative

If you prefer plain `kubectl`, see [`deployment.yaml`](deployment.yaml) for reference manifests. Replace all `${...}` placeholders with actual values before applying.
