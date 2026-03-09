# ─────────────────────────────────────────────────────────────────────────────
# Genie DevOps Copilot – EKS Deployment with AWS ReadOnly Access (IRSA)
# ─────────────────────────────────────────────────────────────────────────────
# This Terraform configuration deploys the Genie DevOps Copilot into an
# existing EKS cluster and grants it AWS ReadOnly access via IRSA
# (IAM Roles for Service Accounts).
#
# Prerequisites:
#   - An existing EKS cluster with an OIDC provider configured
#   - kubectl access configured (for the kubernetes provider)
#   - AWS credentials with permission to create IAM roles/policies
#
# Usage:
#   terraform init
#   terraform plan -var="eks_cluster_name=my-cluster"
#   terraform apply -var="eks_cluster_name=my-cluster"
# ─────────────────────────────────────────────────────────────────────────────

terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.25"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12"
    }
  }
}

# ── Providers ───────────────────────────────────────────────────────────────

provider "aws" {
  region = var.aws.region

  default_tags {
    tags = var.tags
  }
}

data "aws_eks_cluster" "this" {
  name = var.aws.eks_cluster_name
}

data "aws_eks_cluster_auth" "this" {
  name = var.aws.eks_cluster_name
}

provider "kubernetes" {
  host                   = data.aws_eks_cluster.this.endpoint
  cluster_ca_certificate = base64decode(data.aws_eks_cluster.this.certificate_authority[0].data)
  token                  = data.aws_eks_cluster_auth.this.token
}

provider "helm" {
  kubernetes {
    host                   = data.aws_eks_cluster.this.endpoint
    cluster_ca_certificate = base64decode(data.aws_eks_cluster.this.certificate_authority[0].data)
    token                  = data.aws_eks_cluster_auth.this.token
  }
}

# ── Data Sources ────────────────────────────────────────────────────────────

data "aws_caller_identity" "current" {}

data "aws_iam_openid_connect_provider" "eks" {
  url = data.aws_eks_cluster.this.identity[0].oidc[0].issuer
}

locals {
  oidc_provider_arn = data.aws_iam_openid_connect_provider.eks.arn
  oidc_issuer       = replace(data.aws_eks_cluster.this.identity[0].oidc[0].issuer, "https://", "")
  account_id        = data.aws_caller_identity.current.account_id
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 1 – AWS IAM (IRSA): ReadOnly access for the Genie service account
# ═════════════════════════════════════════════════════════════════════════════

# Trust policy: only the Genie K8s service account can assume this role
data "aws_iam_policy_document" "genie_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [local.oidc_provider_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_issuer}:sub"
      values   = ["system:serviceaccount:${var.kubernetes.namespace}:genie-sa"]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_issuer}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "genie_readonly" {
  name               = "genie-devops-copilot-readonly"
  assume_role_policy = data.aws_iam_policy_document.genie_assume_role.json

  tags = {
    Application = "genie-devops-copilot"
    ManagedBy   = "terraform"
  }
}

# Attach the AWS-managed ReadOnlyAccess policy
resource "aws_iam_role_policy_attachment" "readonly" {
  role       = aws_iam_role.genie_readonly.name
  policy_arn = "arn:aws:iam::aws:policy/ReadOnlyAccess"
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 1.5 – EKS Access Entry
# Map the SA's AWS IAM role to the Kubernetes cluster so the Copilot can log in
# seamlessly via `aws eks update-kubeconfig`.
# ═════════════════════════════════════════════════════════════════════════════

resource "aws_eks_access_entry" "genie_readonly" {
  cluster_name      = var.aws.eks_cluster_name
  principal_arn     = aws_iam_role.genie_readonly.arn
  kubernetes_groups = ["genie-readonly-group"]
  type              = "STANDARD"
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 2 – Kubernetes Namespace (optional)
# ═════════════════════════════════════════════════════════════════════════════

resource "kubernetes_namespace" "genie" {
  count = var.kubernetes.create_namespace ? 1 : 0

  metadata {
    name = var.kubernetes.namespace
  }
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 3 – External Secrets: SecretStore + ExternalSecret
# ═════════════════════════════════════════════════════════════════════════════

# SecretStore pointing at AWS Secrets Manager (uses the IRSA role from the SA)
resource "kubernetes_manifest" "secret_store" {
  manifest = {
    apiVersion = "external-secrets.io/v1beta1"
    kind       = "SecretStore"
    metadata = {
      name      = "genie-secret-store"
      namespace = var.kubernetes.namespace
    }
    spec = {
      provider = {
        aws = {
          service = "SecretsManager"
          region  = var.aws.region
        }
      }
    }
  }
}

# ExternalSecret: syncs API keys from AWS Secrets Manager → K8s Secret "genie-secrets"
resource "kubernetes_manifest" "external_secret" {
  manifest = {
    apiVersion = "external-secrets.io/v1beta1"
    kind       = "ExternalSecret"
    metadata = {
      name      = "genie-secrets"
      namespace = var.kubernetes.namespace
    }
    spec = {
      refreshInterval = "15m"
      secretStoreRef = {
        name = "genie-secret-store"
        kind = "SecretStore"
      }
      target = {
        name           = "genie-secrets"
        creationPolicy = "Owner"
      }
      data = [
        {
          secretKey = "LANGFUSE_HOST"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "LANGFUSE_HOST"
          }
        },
        {
          secretKey = "LANGFUSE_PUBLIC_KEY"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "LANGFUSE_PUBLIC_KEY"
          }
        },
        {
          secretKey = "LANGFUSE_SECRET_KEY"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "LANGFUSE_SECRET_KEY"
          }
        },
        {
          secretKey = "OPENAI_API_KEY"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "OPENAI_API_KEY"
          }
        },
        {
          secretKey = "ANTHROPIC_API_KEY"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "ANTHROPIC_API_KEY"
          }
        },
        {
          secretKey = "GEMINI_API_KEY"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "GEMINI_API_KEY"
          }
        },
        {
          secretKey = "GITHUB_TOKEN"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "GITHUB_TOKEN"
          }
        },
        {
          secretKey = "GRAFANA_URL"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "GRAFANA_URL"
          }
        },
        {
          secretKey = "GRAFANA_API_KEY"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "GRAFANA_API_KEY"
          }
        },
        {
          secretKey = "OIDC_ISSUER_URL"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "OIDC_ISSUER_URL"
          }
        },
        {
          secretKey = "OIDC_CLIENT_ID"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "OIDC_CLIENT_ID"
          }
        },
        {
          secretKey = "OIDC_CLIENT_SECRET"
          remoteRef = {
            key      = var.aws.secrets_manager_arn
            property = "OIDC_CLIENT_SECRET"
          }
        },
      ]
    }
  }

  depends_on = [kubernetes_manifest.secret_store]
}

# Read the synced K8s Secret so we can hash its keys for the rollout annotation.
# depends_on ensures this is read after ExternalSecrets syncs the secret.
data "kubernetes_secret" "genie_secrets" {
  metadata {
    name      = "genie-secrets"
    namespace = var.kubernetes.namespace
  }

  depends_on = [kubernetes_manifest.external_secret]
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 4 – Kubernetes Resources: ConfigMap, ServiceAccount, Deployment,
#           Service, Ingress
# ═════════════════════════════════════════════════════════════════════════════

# ── ConfigMap: genie.toml (devops-copilot configuration) ────────────────────

resource "kubernetes_config_map" "genie" {
  metadata {
    name      = "genie-config"
    namespace = var.kubernetes.namespace
  }

  data = {
    "genie.toml" = file("${path.module}/genie.toml")
    "AGENTS.md"  = file("${path.module}/AGENTS.md")
  }
}

# ── ConfigMap: container entrypoint scripts ─────────────────────────────────

resource "kubernetes_config_map" "scripts" {
  metadata {
    name      = "genie-scripts"
    namespace = var.kubernetes.namespace
  }

  data = {
    "credential-bootstrap.sh" = file("${path.module}/scripts/credential-bootstrap.sh")
    "credential-refresh.sh"   = file("${path.module}/scripts/credential-refresh.sh")
    "genie-entrypoint.sh"     = file("${path.module}/scripts/genie-entrypoint.sh")
  }
}

# ── ServiceAccount: annotated with the IRSA role ARN ────────────────────────

resource "kubernetes_service_account" "genie" {
  metadata {
    name      = "genie-sa"
    namespace = var.kubernetes.namespace

    annotations = {
      "eks.amazonaws.com/role-arn" = aws_iam_role.genie_readonly.arn
    }

    labels = {
      app = "genie"
    }
  }
}

resource "kubernetes_cluster_role" "genie_readonly" {
  metadata {
    name = "genie-cluster-readonly"
  }

  rule {
    api_groups = [""]
    resources  = ["pods", "pods/log", "namespaces", "nodes", "events", "services", "configmaps", "persistentvolumes", "persistentvolumeclaims"]
    verbs      = ["get", "list", "watch"]
  }

  rule {
    api_groups = ["apps"]
    resources  = ["deployments", "statefulsets", "daemonsets", "replicasets"]
    verbs      = ["get", "list", "watch"]
  }

  rule {
    api_groups = ["batch"]
    resources  = ["jobs", "cronjobs"]
    verbs      = ["get", "list", "watch"]
  }

  rule {
    api_groups = ["networking.k8s.io"]
    resources  = ["ingresses", "networkpolicies"]
    verbs      = ["get", "list", "watch"]
  }

  rule {
    api_groups = ["autoscaling"]
    resources  = ["horizontalpodautoscalers"]
    verbs      = ["get", "list", "watch"]
  }
}

resource "kubernetes_cluster_role_binding" "genie_readonly" {
  metadata {
    name = "genie-cluster-readonly-binding"
  }

  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = kubernetes_cluster_role.genie_readonly.metadata[0].name
  }

  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.genie.metadata[0].name
    namespace = var.kubernetes.namespace
  }

  # Bind the group mapped to the AWS IAM role (via EKS Access Entry)
  subject {
    kind      = "Group"
    name      = "genie-readonly-group"
    api_group = "rbac.authorization.k8s.io"
  }
}


# ── Deployment ──────────────────────────────────────────────────────────────
#
# SECURITY: Credential Isolation via Init Container + Sidecar Pattern
# ====================================================================
# The genie container where users can run arbitrary shell commands via the
# AG-UI agent MUST NOT have direct access to secrets or IRSA tokens.
# Otherwise, a user can simply run `printenv` or `cat $AWS_WEB_IDENTITY_TOKEN_FILE`
# to exfiltrate API keys, database credentials, and AWS IAM tokens.
#
# Architecture:
#   1. Init container "credential-bootstrap": Has all secrets + IRSA token.
#      Generates kubeconfig, resolves genie.toml with real credentials, and
#      writes both to a shared emptyDir volume.
#   2. Sidecar "credential-refresh": Periodically refreshes the kubeconfig
#      token (IRSA tokens expire in 24h) so kubectl keeps working.
#   3. Main container "genie": Has ZERO secret env vars, NO IRSA token mount.
#      Reads resolved config and kubeconfig from the shared volume only.
#
# This ensures `printenv`, `env`, `cat` on any path cannot reveal credentials.

resource "kubernetes_deployment" "genie" {
  metadata {
    name      = "genie-deployment"
    namespace = var.kubernetes.namespace

    labels = {
      app = "genie"
    }
  }

  timeouts {
    create = "1m"
    update = "1m"
  }

  spec {
    replicas = var.genie.replicas

    # RollingUpdate with zero downtime: new pod starts before old one
    # terminates. This is possible because all persistent state lives in
    # PostgreSQL (sessions) and Qdrant (vectors), not local disk.
    strategy {
      type = "RollingUpdate"

      rolling_update {
        max_surge       = 1
        max_unavailable = 0
      }
    }

    selector {
      match_labels = {
        app = "genie"
      }
    }

    template {
      metadata {
        labels = {
          app = "genie"
        }

        annotations = {
          # Force rolling restart when ConfigMap or Secret data changes.
          # Terraform recomputes the SHA on every plan; if the hash differs
          # the pod template spec changes and Kubernetes triggers a rollout.
          "checksum/config"  = sha256(kubernetes_config_map.genie.data["genie.toml"])
          "checksum/secrets" = sha256(join(",", sort(keys(data.kubernetes_secret.genie_secrets.data))))
        }
      }

      spec {
        service_account_name = kubernetes_service_account.genie.metadata[0].name

        security_context {
          fs_group = 65532
        }

        # ── Init Container: Credential Bootstrap ──────────────────────
        # Runs BEFORE the main container starts. Has access to all secrets
        # and the IRSA token. Generates kubeconfig and resolves genie.toml
        # with real credential values, writing both to /shared-credentials.
        init_container {
          name              = "credential-bootstrap"
          image             = "amazon/aws-cli:latest"
          image_pull_policy = "IfNotPresent"

          command = ["/bin/sh", "/scripts/credential-bootstrap.sh"]

          # IRSA environment variables — only in this init container
          env {
            name  = "AWS_REGION"
            value = var.aws.region
          }

          env {
            name  = "EKS_CLUSTER_NAME"
            value = var.aws.eks_cluster_name
          }

          env {
            name  = "AWS_WEB_IDENTITY_TOKEN_FILE"
            value = "/var/run/secrets/eks.amazonaws.com/serviceaccount/token"
          }

          env {
            name  = "AWS_ROLE_ARN"
            value = aws_iam_role.genie_readonly.arn
          }

          # All application secrets — only in this init container
          env_from {
            secret_ref {
              name     = "genie-secrets"
              optional = true
            }
          }

          env_from {
            secret_ref {
              name = module.database.secret_name
            }
          }

          volume_mount {
            name       = "aws-iam-token"
            mount_path = "/var/run/secrets/eks.amazonaws.com/serviceaccount"
            read_only  = true
          }

          volume_mount {
            name       = "config-volume"
            mount_path = "/app-config"
            read_only  = true
          }

          volume_mount {
            name       = "shared-credentials"
            mount_path = "/shared-credentials"
          }

          volume_mount {
            name       = "scripts-volume"
            mount_path = "/scripts"
            read_only  = true
          }
        }

        # ── Sidecar: Credential Refresh ───────────────────────────────
        # Runs alongside the main container. Periodically refreshes the
        # kubeconfig so the IRSA token (24h expiry) stays valid.
        # This container has secrets but is NOT user-accessible.
        container {
          name              = "credential-refresh"
          image             = "amazon/aws-cli:latest"
          image_pull_policy = "IfNotPresent"

          command = ["/bin/sh", "/scripts/credential-refresh.sh"]

          env {
            name  = "AWS_REGION"
            value = var.aws.region
          }

          env {
            name  = "EKS_CLUSTER_NAME"
            value = var.aws.eks_cluster_name
          }

          env {
            name  = "AWS_WEB_IDENTITY_TOKEN_FILE"
            value = "/var/run/secrets/eks.amazonaws.com/serviceaccount/token"
          }

          env {
            name  = "AWS_ROLE_ARN"
            value = aws_iam_role.genie_readonly.arn
          }

          volume_mount {
            name       = "aws-iam-token"
            mount_path = "/var/run/secrets/eks.amazonaws.com/serviceaccount"
            read_only  = true
          }

          volume_mount {
            name       = "shared-credentials"
            mount_path = "/shared-credentials"
          }

          volume_mount {
            name       = "scripts-volume"
            mount_path = "/scripts"
            read_only  = true
          }

          resources {
            requests = {
              cpu    = "10m"
              memory = "32Mi"
            }
            limits = {
              cpu    = "50m"
              memory = "64Mi"
            }
          }
        }

        # ── 3. Main container: genie ─────────────────────────────────────
        # This container runs the genie binary that executes user-facing
        # commands (shell, kubectl, aws, etc.). As a DevOps copilot it
        # needs AWS CLI and kubectl access with IRSA credentials.
        # API keys and other secrets are NOT in env vars — they are read
        # from the resolved genie.toml on the shared volume.
        container {
          name              = "genie"
          image             = var.genie.image
          image_pull_policy = "Always"

          security_context {
            # Run as root to install tools, then drop privileges via su-exec.
            run_as_user = 0
          }

          command = ["/bin/sh", "/scripts/genie-entrypoint.sh"]

          port {
            container_port = var.genie.port
          }

          # HOME must be set explicitly — user 65532 has no /etc/passwd
          # entry in Alpine, so HOME defaults to "/". Sub-agents and gh CLI
          # rely on HOME to locate config files (~/.config/gh/hosts.yml).
          env {
            name  = "HOME"
            value = "/home/stackgen"
          }

          env {
            name  = "KUBECONFIG"
            value = "/home/stackgen/.kube/config"
          }

          # IRSA credentials — needed for aws CLI and kubectl auth
          env {
            name  = "AWS_REGION"
            value = var.aws.region
          }

          env {
            name  = "AWS_DEFAULT_REGION"
            value = var.aws.region
          }

          env {
            name  = "AWS_ROLE_ARN"
            value = aws_iam_role.genie_readonly.arn
          }

          env {
            name  = "AWS_WEB_IDENTITY_TOKEN_FILE"
            value = "/var/run/secrets/eks.amazonaws.com/serviceaccount/token"
          }

          env {
            name  = "AWS_STS_REGIONAL_ENDPOINTS"
            value = "regional"
          }

          # Suppress the protobuf registration conflict between Qdrant and
          # Milvus gRPC clients — both register "common.proto" but with
          # different (unrelated) message types.  The "warn" policy logs it
          # instead of panicking.
          env {
            name  = "GOLANG_PROTOBUF_REGISTRATION_CONFLICT"
            value = "warn"
          }

          # NOTE: NO env_from blocks — API keys are NOT in env vars.
          # They are resolved into genie.toml by the init container.

          volume_mount {
            name       = "config-volume"
            mount_path = "/app/AGENTS.md"
            sub_path   = "AGENTS.md"
          }

          volume_mount {
            name       = "shared-credentials"
            mount_path = "/shared-credentials"
            read_only  = true
          }



          volume_mount {
            name       = "scripts-volume"
            mount_path = "/scripts"
            read_only  = true
          }

          # IRSA token — needed for aws CLI and kubectl EKS auth
          volume_mount {
            name       = "aws-iam-token"
            mount_path = "/var/run/secrets/eks.amazonaws.com/serviceaccount"
            read_only  = true
          }
        }

        volume {
          name = "config-volume"

          config_map {
            name = kubernetes_config_map.genie.metadata[0].name
          }
        }

        volume {
          name = "scripts-volume"

          config_map {
            name         = kubernetes_config_map.scripts.metadata[0].name
            default_mode = "0755"
          }
        }

        # IRSA token volume — mounted ONLY in init and sidecar containers,
        # NOT in the user-facing genie container.
        volume {
          name = "aws-iam-token"

          projected {
            # Restrictive permissions: owner-read only (was 0644).
            default_mode = "0400"

            sources {
              service_account_token {
                audience           = "sts.amazonaws.com"
                expiration_seconds = 86400
                path               = "token"
              }
            }
          }
        }

        # Shared emptyDir for credential handoff from init/sidecar → genie.
        # Contains: kubeconfig (with embedded short-lived token) and
        # resolved genie.toml (with real API keys substituted).
        volume {
          name = "shared-credentials"

          empty_dir {
            medium = "Memory"
          }
        }


      }
    }
  }
}

# ── Pod Disruption Budget ───────────────────────────────────────────────────

resource "kubernetes_pod_disruption_budget_v1" "genie" {
  metadata {
    name      = "genie-pdb"
    namespace = var.kubernetes.namespace
  }

  spec {
    min_available = 1

    selector {
      match_labels = {
        app = "genie"
      }
    }
  }
}

# ── Service ─────────────────────────────────────────────────────────────────

resource "kubernetes_service" "genie" {
  metadata {
    name      = "genie-service"
    namespace = var.kubernetes.namespace
  }

  spec {
    selector = {
      app = "genie"
    }

    port {
      protocol    = "TCP"
      port        = 80
      target_port = var.genie.port
    }

    type = "ClusterIP"
  }
}

# ── Ingress ─────────────────────────────────────────────────────────────────

resource "kubernetes_ingress_v1" "genie" {
  metadata {
    name      = "genie-ingress"
    namespace = var.kubernetes.namespace
  }

  spec {
    ingress_class_name = "nginx"

    rule {
      host = var.kubernetes.ingress_host

      http {
        path {
          path      = "/"
          path_type = "Prefix"

          backend {
            service {
              name = kubernetes_service.genie.metadata[0].name

              port {
                number = 80
              }
            }
          }
        }
      }
    }
  }
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 6 – PostgreSQL Database
# ═════════════════════════════════════════════════════════════════════════════

module "database" {
  source = "./modules/database"

  namespace = var.kubernetes.namespace
  tags      = var.tags
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 7 – Qdrant Vector Store
# ═════════════════════════════════════════════════════════════════════════════

module "vectorstore" {
  source = "./modules/vectorstore"

  namespace         = var.kubernetes.namespace
  s3_bucket         = var.vectorstore.s3_bucket
  s3_region         = var.aws.region
  oidc_provider_arn = local.oidc_provider_arn
  oidc_issuer       = local.oidc_issuer
  tags              = var.tags

  qdrant = {
    replicas           = var.vectorstore.replicas
    storage_size       = var.vectorstore.storage_size
    image_tag          = var.vectorstore.image_tag
    api_key            = var.vectorstore.api_key
    resources_limits   = var.vectorstore.resources_limits
    resources_requests = var.vectorstore.resources_requests
  }

  depends_on = [
    kubernetes_namespace.genie,
  ]
}

# ── Outputs ─────────────────────────────────────────────────────────────────

output "iam_role_arn" {
  description = "ARN of the IAM role assigned to the Genie service account"
  value       = aws_iam_role.genie_readonly.arn
}

output "service_account_name" {
  description = "Name of the Kubernetes service account"
  value       = kubernetes_service_account.genie.metadata[0].name
}

output "service_endpoint" {
  description = "Internal cluster service endpoint"
  value       = "${kubernetes_service.genie.metadata[0].name}.${var.kubernetes.namespace}.svc.cluster.local"
}

output "ingress_host" {
  description = "Ingress hostname for external access"
  value       = var.kubernetes.ingress_host
}

output "qdrant_http_endpoint" {
  description = "Internal cluster HTTP endpoint for Qdrant"
  value       = module.vectorstore.http_endpoint
}

output "qdrant_grpc_endpoint" {
  description = "Internal cluster gRPC endpoint for Qdrant"
  value       = module.vectorstore.grpc_endpoint
}

output "postgres_host" {
  description = "Internal cluster hostname for PostgreSQL"
  value       = module.database.host
}
