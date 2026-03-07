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
  }
}

# ── Providers ───────────────────────────────────────────────────────────────

provider "aws" {
  region = var.aws.region
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
      ]
    }
  }

  depends_on = [kubernetes_manifest.secret_store]
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
  }
}

# ── Secret: Local Auth Credentials ──────────────────────────────────────────

resource "kubernetes_secret" "genie_local_secrets" {
  metadata {
    name      = "genie-local-secrets"
    namespace = var.kubernetes.namespace
  }

  data = {
    AGUI_PASSWORD      = var.auth.password
    OIDC_ISSUER_URL    = var.auth.oidc_issuer_url
    OIDC_CLIENT_ID     = var.auth.oidc_client_id
    OIDC_CLIENT_SECRET = var.auth.oidc_client_secret
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

# ── Deployment ──────────────────────────────────────────────────────────────

resource "kubernetes_deployment" "genie" {
  metadata {
    name      = "genie-deployment"
    namespace = var.kubernetes.namespace

    labels = {
      app = "genie"
    }
  }

  spec {
    replicas = var.genie.replicas

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
          image             = var.genie.image
          image_pull_policy = "Always"

          port {
            container_port = var.genie.port
          }

          env_from {
            secret_ref {
              name     = "genie-secrets"
              optional = true
            }
          }

          env_from {
            secret_ref {
              name     = kubernetes_secret.genie_local_secrets.metadata[0].name
              optional = true
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
