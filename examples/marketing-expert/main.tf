# ─────────────────────────────────────────────────────────────────────────────
# Genie Marketing Expert – EKS Deployment (Slack-Native)
# ─────────────────────────────────────────────────────────────────────────────
# Deploys the Genie Marketing Intelligence Agent into an existing EKS cluster.
# Unlike the DevOps Copilot, this agent does NOT need AWS API access or
# kubectl — it communicates via Slack and reads Google Drive / Sybill APIs.
#
# Prerequisites:
#   - An existing EKS cluster with an OIDC provider (for IRSA on secrets only)
#   - The Qdrant vector store already deployed (shared with devops-copilot)
#   - kubectl access configured (for the kubernetes provider)
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

# ── Data Sources ────────────────────────────────────────────────────────────

data "aws_iam_openid_connect_provider" "eks" {
  url = data.aws_eks_cluster.this.identity[0].oidc[0].issuer
}

locals {
  oidc_provider_arn = data.aws_iam_openid_connect_provider.eks.arn
  oidc_issuer       = replace(data.aws_eks_cluster.this.identity[0].oidc[0].issuer, "https://", "")
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 1 – IAM Role (minimal: Secrets Manager read only)
# ═════════════════════════════════════════════════════════════════════════════

data "aws_iam_policy_document" "marketing_assume_role" {
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
      values   = ["system:serviceaccount:${var.kubernetes.namespace}:marketing-expert-sa"]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_issuer}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "marketing_secrets" {
  name               = "genie-marketing-expert-secrets"
  assume_role_policy = data.aws_iam_policy_document.marketing_assume_role.json

  tags = {
    Application = "genie-marketing-expert"
    ManagedBy   = "terraform"
  }
}

resource "aws_iam_role_policy" "marketing_secret_access" {
  name = "marketing-secret-access"
  role = aws_iam_role.marketing_secrets.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action   = ["secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"]
        Effect   = "Allow"
        Resource = "${var.aws.secrets_manager_arn}-*"
      }
    ]
  })
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 2 – Reuse Existing PostgreSQL (create a new database inside it)
# ═════════════════════════════════════════════════════════════════════════════
# The devops-copilot already deployed a PostgreSQL StatefulSet in this namespace.
# Instead of deploying a second Postgres, we create a new database (genie_marketing)
# inside the existing instance using a one-shot Kubernetes Job.

resource "kubernetes_job" "create_marketing_db" {
  metadata {
    name      = "create-marketing-db"
    namespace = var.kubernetes.namespace

    labels = {
      app = "marketing-expert"
    }
  }

  spec {
    backoff_limit              = 3
    ttl_seconds_after_finished = 60

    template {
      metadata {
        labels = {
          app = "marketing-expert"
        }
      }

      spec {
        restart_policy = "OnFailure"

        container {
          name  = "create-db"
          image = "postgres:16-alpine"

          command = ["/bin/sh", "-c"]
          args = [
            <<-EOT
            # Wait for PostgreSQL to be ready
            until pg_isready -h postgres.${var.kubernetes.namespace}.svc.cluster.local -U $POSTGRES_USER; do
              echo "Waiting for PostgreSQL..."
              sleep 2
            done

            # Create database if it doesn't exist
            PGPASSWORD=$POSTGRES_PASSWORD psql \
              -h postgres.${var.kubernetes.namespace}.svc.cluster.local \
              -U $POSTGRES_USER \
              -tc "SELECT 1 FROM pg_database WHERE datname = '${var.postgres.db_name}'" \
              | grep -q 1 \
              || PGPASSWORD=$POSTGRES_PASSWORD psql \
                -h postgres.${var.kubernetes.namespace}.svc.cluster.local \
                -U $POSTGRES_USER \
                -c "CREATE DATABASE ${var.postgres.db_name}"

            echo "Database '${var.postgres.db_name}' is ready."
            EOT
          ]

          env_from {
            secret_ref {
              name = "postgres-credentials"
            }
          }
        }
      }
    }
  }

  wait_for_completion = true

  timeouts {
    create = "2m"
  }
}

# Build the DSN for the marketing-specific database, reusing the existing
# PostgreSQL credentials. The password is injected at runtime via envsubst.
locals {
  postgres_dsn = "postgres://genie:$${POSTGRES_PASSWORD}@postgres.${var.kubernetes.namespace}.svc.cluster.local:5432/${var.postgres.db_name}?sslmode=disable"
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 3 – Kubernetes Resources
# ═════════════════════════════════════════════════════════════════════════════

# ── ConfigMap: genie.toml (marketing-expert configuration) ──────────────────

locals {
  genie_toml_rendered = templatefile("${path.module}/genie.toml.tftpl", {
    secrets_manager_name = var.aws.secrets_manager_name
    aws_region           = var.aws.region
    qdrant_host          = var.qdrant.host
    qdrant_port          = var.qdrant.port
    qdrant_api_key       = var.qdrant.api_key
    agui_port            = var.genie.port
  })
}

resource "kubernetes_config_map" "marketing" {
  metadata {
    name      = "marketing-expert-config"
    namespace = var.kubernetes.namespace
  }

  data = {
    "genie.toml" = local.genie_toml_rendered
    "AGENTS.md"  = file("${path.module}/AGENTS.md")
  }
}

# ── ConfigMap: container entrypoint scripts ─────────────────────────────────

resource "kubernetes_config_map" "scripts" {
  metadata {
    name      = "marketing-expert-scripts"
    namespace = var.kubernetes.namespace
  }

  data = {
    "credential-bootstrap.sh" = file("${path.module}/scripts/credential-bootstrap.sh")
    "genie-entrypoint.sh"     = file("${path.module}/scripts/genie-entrypoint.sh")
  }
}

# ── ServiceAccount ──────────────────────────────────────────────────────────

resource "kubernetes_service_account" "marketing" {
  metadata {
    name      = "marketing-expert-sa"
    namespace = var.kubernetes.namespace

    annotations = {
      "eks.amazonaws.com/role-arn" = aws_iam_role.marketing_secrets.arn
    }

    labels = {
      app = "marketing-expert"
    }
  }
}

# ── Deployment ──────────────────────────────────────────────────────────────
#
# SIMPLER than devops-copilot:
#   - No credential-refresh sidecar (no IRSA token rotation needed)
#   - No kubectl/aws CLI tools (marketing agent doesn't need shell)
#   - No cluster RBAC (no K8s API access)
#   - Init container only resolves genie.toml with POSTGRES_DSN

resource "kubernetes_deployment" "marketing" {
  depends_on = [kubernetes_job.create_marketing_db]

  metadata {
    name      = "marketing-expert-deployment"
    namespace = var.kubernetes.namespace

    labels = {
      app = "marketing-expert"
    }
  }

  timeouts {
    create = "1m"
    update = "1m"
  }

  spec {
    replicas = var.genie.replicas

    strategy {
      type = "RollingUpdate"

      rolling_update {
        max_surge       = 1
        max_unavailable = 0
      }
    }

    selector {
      match_labels = {
        app = "marketing-expert"
      }
    }

    template {
      metadata {
        labels = {
          app = "marketing-expert"
        }

        annotations = {
          "checksum/config" = sha256(local.genie_toml_rendered)
        }
      }

      spec {
        service_account_name = kubernetes_service_account.marketing.metadata[0].name

        security_context {
          fs_group = 65532
        }

        # ── Init Container: Credential Bootstrap ──────────────────────
        init_container {
          name              = "credential-bootstrap"
          image             = "amazon/aws-cli:latest"
          image_pull_policy = "IfNotPresent"

          command = ["/bin/sh", "/scripts/credential-bootstrap.sh"]

          env {
            name  = "AWS_REGION"
            value = var.aws.region
          }

          env {
            name  = "AWS_WEB_IDENTITY_TOKEN_FILE"
            value = "/var/run/secrets/eks.amazonaws.com/serviceaccount/token"
          }

          env {
            name  = "AWS_ROLE_ARN"
            value = aws_iam_role.marketing_secrets.arn
          }

          env_from {
            secret_ref {
              name = "postgres-credentials"
            }
          }

          env {
            name  = "NAMESPACE"
            value = var.kubernetes.namespace
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

        # ── Main container: genie ─────────────────────────────────────
        container {
          name              = "genie"
          image             = var.genie.image
          image_pull_policy = "Always"

          command = ["/bin/sh", "/scripts/genie-entrypoint.sh"]

          port {
            container_port = var.genie.port
          }

          env {
            name  = "HOME"
            value = "/home/stackgen"
          }

          env {
            name  = "TMPDIR"
            value = "/tmp"
          }

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
        }

        volume {
          name = "config-volume"

          config_map {
            name = kubernetes_config_map.marketing.metadata[0].name
          }
        }

        volume {
          name = "scripts-volume"

          config_map {
            name         = kubernetes_config_map.scripts.metadata[0].name
            default_mode = "0755"
          }
        }

        volume {
          name = "aws-iam-token"

          projected {
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

resource "kubernetes_pod_disruption_budget_v1" "marketing" {
  metadata {
    name      = "marketing-expert-pdb"
    namespace = var.kubernetes.namespace
  }

  spec {
    min_available = 1

    selector {
      match_labels = {
        app = "marketing-expert"
      }
    }
  }
}

# ── Service ─────────────────────────────────────────────────────────────────

resource "kubernetes_service" "marketing" {
  metadata {
    name      = "marketing-expert-service"
    namespace = var.kubernetes.namespace
  }

  spec {
    selector = {
      app = "marketing-expert"
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

resource "kubernetes_ingress_v1" "marketing" {
  metadata {
    name      = "marketing-expert-ingress"
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
              name = kubernetes_service.marketing.metadata[0].name

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

# ── Outputs ─────────────────────────────────────────────────────────────────

output "iam_role_arn" {
  description = "ARN of the IAM role for the marketing-expert service account"
  value       = aws_iam_role.marketing_secrets.arn
}

output "service_account_name" {
  description = "Name of the Kubernetes service account"
  value       = kubernetes_service_account.marketing.metadata[0].name
}

output "service_endpoint" {
  description = "Internal cluster service endpoint"
  value       = "${kubernetes_service.marketing.metadata[0].name}.${var.kubernetes.namespace}.svc.cluster.local"
}
