# Genie DevOps Copilot – Kubernetes Deployment

Deploy the [Genie](https://github.com/stackgenhq/genie) DevOps Copilot to an EKS cluster with **AWS ReadOnly access** via IRSA, **secret management** via AWS Secrets Manager + External Secrets Operator, **PostgreSQL** for persistent sessions, and **Qdrant** for vector memory.

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│  AWS                                                                     │
│                                                                          │
│  ┌──────────────────────┐    ┌──────────────────────────────┐            │
│  │ IAM Role (IRSA)      │    │ Secrets Manager              │            │
│  │ • ReadOnlyAccess     │    │ platform/secrets/dev/genie   │            │
│  │                      │    │ ┌──────────────────────────┐ │            │
│  └──────────┬───────────┘    │ │ OPENAI_API_KEY           │ │            │
│             │                │ │ ANTHROPIC_API_KEY         │ │            │
│             │ assume-role    │ │ GEMINI_API_KEY            │ │            │
│             │                │ │ GITHUB_TOKEN              │ │            │
│  ┌──────────▼───────────┐    │ │ OIDC_ISSUER_URL          │ │            │
│  │ EKS: developer-eks   │    │ │ OIDC_CLIENT_ID/SECRET    │ │            │
│  │ namespace: genie     │    │ │ LANGFUSE_*               │ │            │
│  │                      │    │ │ GRAFANA_URL / API_KEY     │ │            │
│  │                      │    │ └──────────────────────────┘ │            │
│  │                      │    └──────────────┬───────────────┘            │
│  │ ┌──────────────────┐ │                   │                            │
│  │ │ ServiceAccount   │◄├───────────────────┘                            │
│  │ │ genie-sa (IRSA)  │ │   ExternalSecret sync                         │
│  │ └────────┬─────────┘ │                                                │
│  │          │            │                                                │
│  │ ┌────────▼───────────────────────────────────────────────┐            │
│  │ │ Deployment: genie-deployment (Recreate strategy)       │            │
│  │ │                                                        │            │
│  │ │  ┌─────────────────────┐  ┌──────────────────────┐    │            │
│  │ │  │ Init: credential-   │  │ Sidecar: credential- │    │            │
│  │ │  │ bootstrap           │  │ refresh              │    │            │
│  │ │  │ • Has secrets+IRSA  │  │ • Refreshes kube-    │    │            │
│  │ │  │ • Generates kube-   │  │   config token       │    │            │
│  │ │  │   config            │  │   (IRSA 24h expiry)  │    │            │
│  │ │  │ • Resolves genie-   │  └──────────────────────┘    │            │
│  │ │  │   .toml (envsubst)  │                               │            │
│  │ │  └─────────────────────┘  ┌──────────────────────┐    │            │
│  │ │                           │ Main: genie           │    │            │
│  │ │                           │ • ZERO secret env     │    │            │
│  │ │                           │ • Reads config from   │    │            │
│  │ │                           │   shared volume       │    │            │
│  │ │                           │ • Port 9876           │    │            │
│  │ │                           └──────────────────────┘    │            │
│  │ └───────────────────────────────┬────────────────────────┘            │
│  │          │                      │                                     │
│  │ ┌────────▼─────────┐  ┌────────▼─────────┐  ┌──────────────────┐    │
│  │ │ PostgreSQL        │  │ Qdrant (Helm)    │  │ PVC: genie-data  │    │
│  │ │ StatefulSet       │  │ StatefulSet      │  │ 10Gi             │    │
│  │ │ :5432             │  │ HTTP :6333       │  │ (ReadWriteOnce)  │    │
│  │ │                   │  │ gRPC :6334       │  └──────────────────┘    │
│  │ └──────────────────┘  └──────────────────┘                           │
│  │          │                                                            │
│  │ ┌────────▼─────────┐                                                 │
│  │ │ Service (ClusterIP)                                                │
│  │ │ :80 → :9876      │                                                 │
│  │ └────────┬─────────┘                                                 │
│  │          │                                                            │
│  │ ┌────────▼─────────┐                                                 │
│  │ │ Ingress (nginx)  │                                                 │
│  │ │ genie.dev.       │                                                 │
│  │ │ stackgen.com     │                                                 │
│  │ └─────────────────┘                                                  │
│  └──────────────────────┘                                                │
└──────────────────────────────────────────────────────────────────────────┘
```

### Security: Credential Isolation

The deployment uses an **init container + sidecar** architecture to ensure **no secrets are accessible** from the user-facing genie container:

1. **Init container** (`credential-bootstrap`): Has all secrets + IRSA token. Generates kubeconfig, resolves `genie.toml` with real credentials via `envsubst`, writes both to a shared emptyDir volume.
2. **Sidecar** (`credential-refresh`): Periodically refreshes the kubeconfig token (IRSA tokens expire in 24h).
3. **Main container** (`genie`): Has **zero** secret env vars, **no** IRSA token mount. Reads resolved config from the shared volume only. Running `printenv` or `cat` from within this container will **not** reveal any credentials.

## Prerequisites

| Requirement | Details |
|---|---|
| **EKS Cluster** | With [OIDC provider](https://docs.aws.amazon.com/eks/latest/userguide/enable-iam-roles-for-service-accounts.html) configured |
| **External Secrets Operator** | [Installed](https://external-secrets.io/latest/introduction/getting-started/) on the cluster |
| **NGINX Ingress Controller** | Running on the cluster with `ingressClassName: nginx` |
| **AWS CLI / Profile** | Configured with admin access to the target account (SSO supported) |
| **OpenTofu** | ≥ 1.5 (or Terraform ≥ 1.5) |

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
    "GRAFANA_API_KEY": "glsa_...",
    "LANGFUSE_HOST": "https://langfuse.example.com",
    "LANGFUSE_PUBLIC_KEY": "pk-...",
    "LANGFUSE_SECRET_KEY": "sk-...",
    "OIDC_ISSUER_URL": "https://auth.example.com",
    "OIDC_CLIENT_ID": "genie-client-id",
    "OIDC_CLIENT_SECRET": "genie-client-secret"
  }'
```

> **Note:** The secret ARN will look like:
> `arn:aws:secretsmanager:us-west-2:339712749745:secret:platform/secrets/dev/genie-XXXXXX`
> You'll need the full ARN (including the random suffix) for the `secrets_manager_arn` variable.

### 2. Configure Variables

Edit `dev.auto.tfvars` with your values (auto-loaded by OpenTofu/Terraform):

```hcl
aws = {
  region              = "us-west-2"
  eks_cluster_name    = "developer-eks"
  secrets_manager_arn = "arn:aws:secretsmanager:us-west-2:339712749745:secret:platform/secrets/dev/genie-XXXXXX"
}

kubernetes = {
  namespace        = "genie"
  create_namespace = false        # set true if namespace doesn't exist
  ingress_host     = "genie.dev.stackgen.com"
}

genie = {
  image    = "ghcr.io/stackgenhq/genie-beta:latest"
  replicas = 1
  port     = 9876
}

vectorstore = {
  s3_bucket = "339712749745-qdrant-snapshots"
}

tags = {
  "created_by" = "your-name"
  "for"        = "genie"
  "repo"       = "https://github.com/stackgenhq/genie"
}
```

### 3. Deploy

```bash
# Set your AWS profile (SSO)
export AWS_PROFILE=339712749745_AdministratorAccess

# Initialize OpenTofu (first time only)
tofu init

# Review the plan
tofu plan

# Apply (dev.auto.tfvars is loaded automatically)
tofu apply
```

### 4. Verify

```bash
# Check the ExternalSecret synced successfully
kubectl get externalsecret -n genie

# Check the generated K8s secret
kubectl get secret genie-secrets -n genie

# Check all pods are running
kubectl get pods -n genie

# Check ingress
kubectl get ingress -n genie

# Check genie logs
kubectl logs -n genie -l app=genie -c genie --tail=50
```

## AWS Secrets Manager – Required Keys

The following keys **must** exist in the Secrets Manager secret as a JSON object:

| Key | Description | Required | Example |
|---|---|---|---|
| `OPENAI_API_KEY` | OpenAI API key for GPT models | **Yes** | `sk-proj-...` |
| `ANTHROPIC_API_KEY` | Anthropic API key for Claude models | **Yes** | `sk-ant-api03-...` |
| `GEMINI_API_KEY` | Google Gemini API key | **Yes** | `AIza...` |
| `GITHUB_TOKEN` | GitHub personal access token (for MCP + SCM) | **Yes** | `ghp_...` |
| `LANGFUSE_HOST` | Langfuse tracing host | Optional | `https://langfuse.example.com` |
| `LANGFUSE_PUBLIC_KEY` | Langfuse public key | Optional | `pk-...` |
| `LANGFUSE_SECRET_KEY` | Langfuse secret key | Optional | `sk-...` |
| `OIDC_ISSUER_URL` | OIDC provider issuer URL (for SSO) | Optional | `https://auth.example.com` |
| `OIDC_CLIENT_ID` | OIDC client ID | Optional | `genie-client-id` |
| `OIDC_CLIENT_SECRET` | OIDC client secret | Optional | `secret` |
| `GRAFANA_URL` | Grafana instance URL (for MCP server) | Optional | `https://grafana.example.com` |
| `GRAFANA_API_KEY` | Grafana service account token | Optional | `glsa_...` |

### Updating Secrets

To update a secret value after initial creation:

```bash
aws secretsmanager put-secret-value \
  --secret-id "platform/secrets/dev/genie" \
  --region us-west-2 \
  --secret-string '{ ... }'
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
| `aws_eks_access_entry.genie_readonly` | EKS Access Entry | Maps the SA IAM role to the K8s cluster |
| `kubernetes_namespace.genie` | Namespace | Optional – creates the namespace if `create_namespace = true` |
| `kubernetes_manifest.secret_store` | SecretStore | Connects external-secrets operator to AWS Secrets Manager |
| `kubernetes_manifest.external_secret` | ExternalSecret | Syncs API keys from Secrets Manager into K8s Secret |
| `kubernetes_config_map.genie` | ConfigMap | Mounts `genie.toml` and `AGENTS.md` into the pod |
| `kubernetes_config_map.scripts` | ConfigMap | Entrypoint scripts for init container, sidecar, and main |
| `kubernetes_service_account.genie` | ServiceAccount | Annotated with IRSA role ARN for AWS access |
| `kubernetes_cluster_role.genie_readonly` | ClusterRole | Read-only access to pods, deployments, logs, etc. |
| `kubernetes_cluster_role_binding.genie_readonly` | ClusterRoleBinding | Binds the ClusterRole to the SA and IRSA group |
| `kubernetes_persistent_volume_claim.genie_data` | PVC | 10Gi persistent storage for genie data |
| `kubernetes_deployment.genie` | Deployment | Runs genie with init/sidecar credential isolation |
| `kubernetes_pod_disruption_budget_v1.genie` | PDB | Ensures at least 1 pod is available during disruptions |
| `kubernetes_service.genie` | Service | ClusterIP service (port 80 → 9876) |
| `kubernetes_ingress_v1.genie` | Ingress | NGINX ingress at the configured hostname |
| `module.database` | Module | PostgreSQL StatefulSet + Service + PVC + credentials Secret |
| `module.vectorstore` | Module | Qdrant (Helm) + S3 bucket for snapshots + IRSA |

## Check Logs

```sh
# All containers (init, sidecar, main)
kubectl logs -n genie -l app=genie --tail=100 -f --all-containers --max-log-requests 100

# Genie container only
kubectl logs -n genie -l app=genie -c genie --tail=100 -f

# Init container (credential bootstrap)
kubectl logs -n genie -l app=genie -c credential-bootstrap
```

## Files

```
examples/devops-in-k8s/
├── main.tf              # Main Terraform configuration (source of truth)
├── variables.tf         # Variable definitions (aws, kubernetes, genie, vectorstore)
├── backend.tf           # S3 backend for remote state
├── dev.auto.tfvars      # Auto-loaded variables for the dev cluster
├── genie.toml           # Genie devops-copilot configuration (with ${VAR} placeholders)
├── AGENTS.md            # Agent behavior rules (mounted into the container)
├── scripts/
│   ├── credential-bootstrap.sh  # Init container: generates kubeconfig + resolves config
│   ├── credential-refresh.sh    # Sidecar: periodic kubeconfig token refresh
│   └── genie-entrypoint.sh      # Main container: installs tools + drops privileges
├── modules/
│   ├── database/        # PostgreSQL StatefulSet, Service, PVC, credentials
│   └── vectorstore/     # Qdrant Helm chart, S3 bucket, IRSA for snapshots
└── README.md            # This file
```

## Customization

### Adding More Secrets

To sync additional secrets (e.g., Datadog, PagerDuty), add entries to the `data` block in the `kubernetes_manifest.external_secret` resource in `main.tf`:

```hcl
{
  secretKey = "DD_API_KEY"
  remoteRef = {
    key      = var.aws.secrets_manager_arn
    property = "DD_API_KEY"
  }
},
```

Then add the corresponding key to your AWS Secrets Manager secret and uncomment the relevant MCP server section in `genie.toml`.

### Using a Different Ingress Host

Update the `ingress_host` field in the `kubernetes` variable in your `.auto.tfvars` file. Make sure a DNS record (or wildcard) points to the NGINX ingress controller's load balancer.

### Scaling

The deployment uses a **Recreate** strategy (not RollingUpdate) because the `genie-data` PVC is `ReadWriteOnce` and cannot be mounted by two pods simultaneously. To run multiple replicas, switch to a `ReadWriteMany` storage class or remove the PVC dependency.

## Troubleshooting

### Init container CrashLoopBackOff

Check init container logs:
```bash
kubectl logs -n genie -l app=genie -c credential-bootstrap
```

Common causes:
- **`envsubst: command not found`** — The `credential-bootstrap.sh` script installs `gettext` (which provides `envsubst`) at startup. If this fails, check network connectivity from the pod.
- **IRSA token issues** — Verify the OIDC provider is correctly configured on the EKS cluster.

### Genie container crashes with "permission denied"

The init container writes files to `/shared-credentials`. The `chown 65532:65532` step ensures the genie container (running as uid 65532) can read them. If this fails, check the `credential-bootstrap.sh` script.

### Protobuf registration conflict

If you see `proto: file "common.proto" is already registered`, this is a known benign conflict between the Milvus and Qdrant gRPC clients. The `GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn` env var (set on the genie container) suppresses the panic and logs a warning instead.

### Database migration errors

If you see `type "blob" does not exist`, ensure you're running genie version `≥ 0.1.7-rc.12` which uses database-dialect-aware types for PostgreSQL compatibility.

## Tear Down

```bash
export AWS_PROFILE=339712749745_AdministratorAccess
tofu destroy
```

This will remove all Kubernetes and AWS resources. The AWS Secrets Manager secret is **not** managed by Terraform and will remain.
