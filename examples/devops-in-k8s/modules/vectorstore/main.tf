# ─────────────────────────────────────────────────────────────────────────────
# Qdrant Vector Store Module
# ─────────────────────────────────────────────────────────────────────────────
# Deploys Qdrant into a Kubernetes namespace via the official Helm chart and
# configures S3-backed snapshot storage.  An IRSA role is created so the
# Qdrant pods can read/write to the designated S3 bucket without static
# credentials.
#
# Reference: https://qdrant.tech/documentation/guides/installation/
# ─────────────────────────────────────────────────────────────────────────────

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.25"
    }
  }
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 1 – IAM Role (IRSA) for S3 access
# ═════════════════════════════════════════════════════════════════════════════

data "aws_iam_policy_document" "qdrant_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [var.oidc_provider_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "${var.oidc_issuer}:sub"
      values   = ["system:serviceaccount:${var.namespace}:qdrant"]
    }

    condition {
      test     = "StringEquals"
      variable = "${var.oidc_issuer}:aud"
      values   = ["sts.amazonaws.com"]
    }
  }
}

# ── S3 Bucket for snapshots ─────────────────────────────────────────────────

resource "aws_s3_bucket" "qdrant_snapshots" {
  bucket = var.s3_bucket

  tags = merge(var.tags, {
    Application = "qdrant-vectorstore"
    ManagedBy   = "terraform"
  })
}

resource "aws_s3_bucket_server_side_encryption_configuration" "qdrant_snapshots" {
  bucket = aws_s3_bucket.qdrant_snapshots.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "qdrant_snapshots" {
  bucket = aws_s3_bucket.qdrant_snapshots.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ── IAM Role (IRSA) ────────────────────────────────────────────────────────

resource "aws_iam_role" "qdrant_s3" {
  name               = "qdrant-vectorstore-s3"
  assume_role_policy = data.aws_iam_policy_document.qdrant_assume_role.json

  tags = merge(var.tags, {
    Application = "qdrant-vectorstore"
    ManagedBy   = "terraform"
  })
}

# Policy: allow read/write to the snapshot S3 bucket
data "aws_iam_policy_document" "qdrant_s3" {
  statement {
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
      "s3:ListBucket",
    ]
    resources = [
      "arn:aws:s3:::${var.s3_bucket}",
      "arn:aws:s3:::${var.s3_bucket}/*",
    ]
  }
}

resource "aws_iam_policy" "qdrant_s3" {
  name   = "qdrant-vectorstore-s3-access"
  policy = data.aws_iam_policy_document.qdrant_s3.json

  tags = merge(var.tags, {
    Application = "qdrant-vectorstore"
    ManagedBy   = "terraform"
  })
}

resource "aws_iam_role_policy_attachment" "qdrant_s3" {
  role       = aws_iam_role.qdrant_s3.name
  policy_arn = aws_iam_policy.qdrant_s3.arn
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 2 – Qdrant Helm Release
# ═════════════════════════════════════════════════════════════════════════════
# Uses the official qdrant/qdrant chart.
# S3 snapshot storage is configured via the `config` values block.
# The IRSA annotation is set on the chart-managed ServiceAccount so that
# the pods automatically receive AWS credentials via projected tokens.
# ═════════════════════════════════════════════════════════════════════════════

resource "helm_release" "qdrant" {
  name       = "qdrant"
  repository = "https://qdrant.to/helm"
  chart      = "qdrant"
  namespace  = var.namespace
  version    = "1.17.0" # pinned for reproducibility; review before bumping
  wait = true

  # ── Replicas ──────────────────────────────────────────────────────────────
  set {
    name  = "replicaCount"
    value = var.qdrant.replicas
  }

  # ── Image tag override (optional) ────────────────────────────────────────
  dynamic "set" {
    for_each = var.qdrant.image_tag != "" ? [var.qdrant.image_tag] : []
    content {
      name  = "image.tag"
      value = set.value
    }
  }

  # ── Persistence ──────────────────────────────────────────────────────────
  set {
    name  = "persistence.size"
    value = var.qdrant.storage_size
  }

  # ── Resources ────────────────────────────────────────────────────────────
  set {
    name  = "resources.requests.cpu"
    value = var.qdrant.resources_requests.cpu
  }

  set {
    name  = "resources.requests.memory"
    value = var.qdrant.resources_requests.memory
  }

  set {
    name  = "resources.limits.cpu"
    value = var.qdrant.resources_limits.cpu
  }

  set {
    name  = "resources.limits.memory"
    value = var.qdrant.resources_limits.memory
  }

  # ── API key (optional) ───────────────────────────────────────────────────
  dynamic "set_sensitive" {
    for_each = var.qdrant.api_key != "" ? [var.qdrant.api_key] : []
    content {
      name  = "config.service.api_key"
      value = set_sensitive.value
    }
  }

  # ── Qdrant configuration: S3 snapshot storage + IRSA annotation ──────────
  # Qdrant natively supports S3-backed snapshots since v1.10.
  # The Helm chart exposes config.* which maps directly to the
  # Qdrant YAML config. The serviceAccount.annotations block injects the
  # IRSA role ARN so the pods pick up AWS credentials automatically.
  values = [yamlencode({
    serviceAccount = {
      annotations = {
        "eks.amazonaws.com/role-arn" = aws_iam_role.qdrant_s3.arn
      }
    }
    config = {
      storage = {
        snapshots_config = {
          snapshots_storage = "s3"
          s3_config = {
            bucket = var.s3_bucket
            region = var.s3_region
            # Access keys are not set – Qdrant picks up credentials from
            # the AWS SDK default chain (IRSA projected token).
          }
        }
      }
    }
  })]
}
