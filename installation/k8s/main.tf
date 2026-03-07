# ─────────────────────────────────────────────────────────────────────────────
# Genie – EKS Deployment with AWS ReadOnly Access (IRSA)
# ─────────────────────────────────────────────────────────────────────────────
# This Terraform configuration deploys Genie into an existing EKS cluster
# and optionally grants it AWS ReadOnly access via IRSA (IAM Roles for
# Service Accounts) and syncs secrets from AWS Secrets Manager via the
# External Secrets Operator.
#
# Prerequisites:
#   - An existing EKS cluster with an OIDC provider configured
#   - kubectl access configured (for the kubernetes provider)
#   - AWS credentials with permission to create IAM roles/policies
#   - External Secrets Operator installed on the cluster (if using secrets)
#
# Usage:
#   terraform init
#   terraform plan  -var-file=terraform.tfvars
#   terraform apply -var-file=terraform.tfvars
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
    random = {
      source  = "hashicorp/random"
      version = "~> 3.5"
    }
  }
}

# ── Variables ───────────────────────────────────────────────────────────────

variable "aws_region" {
  description = "AWS region where the EKS cluster lives"
  type        = string
  default     = "us-east-1"
}

variable "eks_cluster_name" {
  description = "Name of the existing EKS cluster"
  type        = string
}

variable "namespace" {
  description = "Kubernetes namespace to deploy Genie into"
  type        = string
  default     = "default"
}

variable "create_namespace" {
  description = "Whether to create the namespace (set false if it already exists)"
  type        = bool
  default     = false
}

variable "genie_image" {
  description = "Container image for the Genie deployment"
  type        = string
  default     = "ghcr.io/stackgenhq/genie:latest"
}

variable "genie_replicas" {
  description = "Number of Genie pod replicas"
  type        = number
  default     = 1
}

variable "ingress_host" {
  description = "Hostname for the Ingress resource"
  type        = string
  default     = "genie.local"
}

variable "ingress_class_name" {
  description = "IngressClass name (e.g. 'nginx', 'alb')"
  type        = string
  default     = "nginx"
}

variable "ingress_annotations" {
  description = "Annotations to add to the Ingress resource"
  type        = map(string)
  default     = {}
}

variable "container_port" {
  description = "Port the Genie server listens on inside the container"
  type        = number
  default     = 9876
}

variable "genie_config_file" {
  description = "Path to the genie.toml configuration file"
  type        = string
  default     = "genie.toml"
}

variable "aws_secrets_manager_arn" {
  description = "ARN of the AWS Secrets Manager secret containing API keys. Required when enable_external_secrets is true."
  type        = string
  default     = ""
}

variable "enable_external_secrets" {
  description = "Whether to create SecretStore + ExternalSecret resources (requires External Secrets Operator)"
  type        = bool
  default     = true
}

variable "enable_irsa" {
  description = "Whether to create an IAM role with ReadOnlyAccess and bind it to the service account via IRSA"
  type        = bool
  default     = true
}

variable "external_secret_keys" {
  description = "Map of K8s secret key names to Secrets Manager property names to sync"
  type        = map(string)
  default = {
    OPENAI_API_KEY    = "OPENAI_API_KEY"
    ANTHROPIC_API_KEY = "ANTHROPIC_API_KEY"
    GEMINI_API_KEY    = "GEMINI_API_KEY"
    GITHUB_TOKEN      = "GITHUB_TOKEN"
    GRAFANA_URL       = "GRAFANA_URL"
    GRAFANA_API_KEY   = "GRAFANA_API_KEY"
    AGUI_PASSWORD     = "AGUI_PASSWORD"
  }
}

# ── Providers ───────────────────────────────────────────────────────────────

provider "aws" {
  region = var.aws_region
}

data "aws_eks_cluster" "this" {
  name = var.eks_cluster_name
}

data "aws_eks_cluster_auth" "this" {
  name = var.eks_cluster_name
}

provider "kubernetes" {
  host                   = data.aws_eks_cluster.this.endpoint
  cluster_ca_certificate = base64decode(data.aws_eks_cluster.this.certificate_authority[0].data)
  token                  = data.aws_eks_cluster_auth.this.token
}

# ── Data Sources ────────────────────────────────────────────────────────────

data "aws_caller_identity" "current" {}

data "aws_iam_openid_connect_provider" "eks" {
  count = var.enable_irsa ? 1 : 0
  url   = data.aws_eks_cluster.this.identity[0].oidc[0].issuer
}

locals {
  oidc_provider_arn = var.enable_irsa ? data.aws_iam_openid_connect_provider.eks[0].arn : ""
  oidc_issuer       = var.enable_irsa ? replace(data.aws_eks_cluster.this.identity[0].oidc[0].issuer, "https://", "") : ""
  account_id        = data.aws_caller_identity.current.account_id
}

# ── Random password (fallback when not in Secrets Manager) ──────────────────

resource "random_password" "agui" {
  length  = 32
  special = false
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 1 – AWS IAM (IRSA): ReadOnly access for the Genie service account
# ═════════════════════════════════════════════════════════════════════════════

# Trust policy: only the Genie K8s service account can assume this role
data "aws_iam_policy_document" "genie_assume_role" {
  count = var.enable_irsa ? 1 : 0

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
      values   = ["system:serviceaccount:${var.namespace}:genie-sa"]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_issuer}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "genie_readonly" {
  count              = var.enable_irsa ? 1 : 0
  name               = "genie-${var.namespace}-readonly"
  assume_role_policy = data.aws_iam_policy_document.genie_assume_role[0].json

  tags = {
    Application = "genie"
    Namespace   = var.namespace
    ManagedBy   = "terraform"
  }
}

# Attach the AWS-managed ReadOnlyAccess policy
resource "aws_iam_role_policy_attachment" "readonly" {
  count      = var.enable_irsa ? 1 : 0
  role       = aws_iam_role.genie_readonly[0].name
  policy_arn = "arn:aws:iam::aws:policy/ReadOnlyAccess"
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 2 – Kubernetes Namespace (optional)
# ═════════════════════════════════════════════════════════════════════════════

resource "kubernetes_namespace" "genie" {
  count = var.create_namespace ? 1 : 0

  metadata {
    name = var.namespace
  }
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 3 – External Secrets: SecretStore + ExternalSecret
# ═════════════════════════════════════════════════════════════════════════════

# SecretStore pointing at AWS Secrets Manager (uses the IRSA role from the SA)
resource "kubernetes_manifest" "secret_store" {
  count = var.enable_external_secrets ? 1 : 0

  manifest = {
    apiVersion = "external-secrets.io/v1beta1"
    kind       = "SecretStore"
    metadata = {
      name      = "genie-secret-store"
      namespace = var.namespace
    }
    spec = {
      provider = {
        aws = {
          service = "SecretsManager"
          region  = var.aws_region
        }
      }
    }
  }
}

# ExternalSecret: syncs API keys from AWS Secrets Manager → K8s Secret "genie-secrets"
resource "kubernetes_manifest" "external_secret" {
  count = var.enable_external_secrets ? 1 : 0

  manifest = {
    apiVersion = "external-secrets.io/v1beta1"
    kind       = "ExternalSecret"
    metadata = {
      name      = "genie-secrets"
      namespace = var.namespace
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
        for k, v in var.external_secret_keys : {
          secretKey = k
          remoteRef = {
            key      = var.aws_secrets_manager_arn
            property = v
          }
        }
      ]
    }
  }

  depends_on = [kubernetes_manifest.secret_store]
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 4 – Kubernetes Resources: ConfigMap, ServiceAccount, Deployment,
#           Service, Ingress
# ═════════════════════════════════════════════════════════════════════════════

# ── ConfigMap: genie.toml ───────────────────────────────────────────────────

resource "kubernetes_config_map" "genie" {
  metadata {
    name      = "genie-config"
    namespace = var.namespace
  }

  data = {
    "genie.toml" = file("${path.module}/${var.genie_config_file}")
  }
}

# ── ServiceAccount ──────────────────────────────────────────────────────────

resource "kubernetes_service_account" "genie" {
  metadata {
    name      = "genie-sa"
    namespace = var.namespace

    annotations = var.enable_irsa ? {
      "eks.amazonaws.com/role-arn" = aws_iam_role.genie_readonly[0].arn
    } : {}

    labels = {
      app = "genie"
    }
  }
}

# ── Deployment ──────────────────────────────────────────────────────────────

resource "kubernetes_deployment" "genie" {
  metadata {
    name      = "genie-deployment"
    namespace = var.namespace

    labels = {
      app = "genie"
    }
  }

  spec {
    replicas = var.genie_replicas

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
      }

      spec {
        service_account_name = kubernetes_service_account.genie.metadata[0].name

        container {
          name              = "genie"
          image             = var.genie_image
          image_pull_policy = "Always"

          port {
            container_port = var.container_port
          }

          dynamic "env_from" {
            for_each = var.enable_external_secrets ? [1] : []
            content {
              secret_ref {
                name     = "genie-secrets"
                optional = true
              }
            }
          }

          # When external secrets are disabled, inject the Terraform-generated
          # AGUI_PASSWORD directly so password_protected always works.
          dynamic "env" {
            for_each = var.enable_external_secrets ? [] : [1]
            content {
              name  = "AGUI_PASSWORD"
              value = random_password.agui.result
            }
          }

          volume_mount {
            name       = "config-volume"
            mount_path = "/app/genie.toml"
            sub_path   = "genie.toml"
          }
        }

        volume {
          name = "config-volume"

          config_map {
            name = kubernetes_config_map.genie.metadata[0].name
          }
        }
      }
    }
  }

  depends_on = [kubernetes_manifest.external_secret]
}

# ── Service ─────────────────────────────────────────────────────────────────

resource "kubernetes_service" "genie" {
  metadata {
    name      = "genie-service"
    namespace = var.namespace
  }

  spec {
    selector = {
      app = "genie"
    }

    port {
      protocol    = "TCP"
      port        = 80
      target_port = var.container_port
    }

    type = "ClusterIP"
  }
}

# ── Ingress ─────────────────────────────────────────────────────────────────

resource "kubernetes_ingress_v1" "genie" {
  metadata {
    name      = "genie-ingress"
    namespace = var.namespace

    annotations = var.ingress_annotations
  }

  spec {
    ingress_class_name = var.ingress_class_name

    rule {
      host = var.ingress_host

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

# ── Outputs ─────────────────────────────────────────────────────────────────

output "iam_role_arn" {
  description = "ARN of the IAM role assigned to the Genie service account (empty if IRSA disabled)"
  value       = var.enable_irsa ? aws_iam_role.genie_readonly[0].arn : ""
}

output "service_account_name" {
  description = "Name of the Kubernetes service account"
  value       = kubernetes_service_account.genie.metadata[0].name
}

output "service_endpoint" {
  description = "Internal cluster service endpoint"
  value       = "${kubernetes_service.genie.metadata[0].name}.${var.namespace}.svc.cluster.local"
}

output "ingress_host" {
  description = "Ingress hostname for external access"
  value       = var.ingress_host
}

output "agui_password" {
  description = "Auto-generated AG-UI password (use when external secrets are disabled or as the value to store in Secrets Manager)"
  value       = random_password.agui.result
  sensitive   = true
}
