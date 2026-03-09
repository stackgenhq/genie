# ─────────────────────────────────────────────────────────────────────────────
# Qdrant Vector Store Module
# ─────────────────────────────────────────────────────────────────────────────
# Deploys Qdrant into a Kubernetes namespace via the official Helm chart with:
#   - Distributed cluster mode (Raft consensus)
#   - Multi-AZ pod spreading via topology constraints & anti-affinity
#   - S3-backed snapshot storage for disaster recovery
#   - PodDisruptionBudget for safe node drains
#   - NetworkPolicy for p2p and client port isolation
#   - Automated snapshot CronJob for periodic backups
#   - IRSA role for credential-less S3 access
#
# Primary data persistence uses EBS-backed PVCs (block storage).
# S3 is used ONLY for snapshot/backup storage — Qdrant requires POSIX-
# compatible block storage for its WAL and memory-mapped files.
#
# Reference: https://qdrant.tech/documentation/guides/distributed_deployment/
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

# ── S3 Bucket versioning ───────────────────────────────────────────────────
# Enables object versioning so overwritten snapshots can be recovered.
# Critical for disaster recovery — without this, a bad snapshot overwrites
# the only copy of the previous good one.

resource "aws_s3_bucket_versioning" "qdrant_snapshots" {
  bucket = aws_s3_bucket.qdrant_snapshots.id

  versioning_configuration {
    status = "Enabled"
  }
}

# ── S3 Lifecycle rules ─────────────────────────────────────────────────────
# Automatically expire old snapshots and non-current versions to control
# storage costs. Without this, snapshots accumulate indefinitely.

resource "aws_s3_bucket_lifecycle_configuration" "qdrant_snapshots" {
  bucket = aws_s3_bucket.qdrant_snapshots.id

  rule {
    id     = "expire-old-snapshots"
    status = "Enabled"

    # Apply to all objects in the bucket
    filter {}

    # Current versions: keep for configured retention period
    expiration {
      days = var.snapshot_retention_days
    }

    # Non-current (overwritten) versions: keep for 7 days
    noncurrent_version_expiration {
      noncurrent_days = 7
    }
  }

  depends_on = [aws_s3_bucket_versioning.qdrant_snapshots]
}

# ── IAM Role (IRSA) ────────────────────────────────────────────────────────
# Names are prefixed with the namespace to allow multiple deployments in the
# same AWS account (e.g. staging + prod) without name collisions.

resource "aws_iam_role" "qdrant_s3" {
  name               = "${var.namespace}-qdrant-vectorstore-s3"
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
  name   = "${var.namespace}-qdrant-vectorstore-s3-access"
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
# PART 2 – StorageClass for EBS gp3 with AZ-aware binding
# ═════════════════════════════════════════════════════════════════════════════
# WaitForFirstConsumer ensures the PV is provisioned in the same AZ where the
# pod is scheduled.  This is critical for multi-AZ StatefulSets because EBS
# volumes are AZ-bound and cannot be attached cross-AZ.
# ═════════════════════════════════════════════════════════════════════════════

resource "kubernetes_storage_class" "qdrant" {
  count = var.create_storage_class ? 1 : 0

  metadata {
    name = var.qdrant.storage_class

    labels = {
      app       = "qdrant"
      ManagedBy = "terraform"
    }
  }

  storage_provisioner    = "ebs.csi.aws.com"
  reclaim_policy         = "Retain"
  volume_binding_mode    = "WaitForFirstConsumer"
  allow_volume_expansion = true

  parameters = {
    type      = "gp3"
    encrypted = "true"
  }
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 3 – Qdrant Helm Release (distributed, multi-AZ)
# ═════════════════════════════════════════════════════════════════════════════
# Uses the official qdrant/qdrant Helm chart with:
#   - Cluster mode enabled (Raft consensus on port 6335)
#   - TopologySpreadConstraints to distribute pods across AZs
#   - Pod anti-affinity to prevent co-location on the same node
#   - PodDisruptionBudget for safe rolling updates and node drains
#   - Liveness and startup probes for deadlock detection
#   - S3 snapshot storage via IRSA (no static credentials)
#
# The Helm chart deploys a StatefulSet; each pod gets its own EBS PVC.
# Data replication is handled at the Qdrant application level (shard
# replication), NOT at the storage level.
# ═════════════════════════════════════════════════════════════════════════════

resource "helm_release" "qdrant" {
  name       = "qdrant"
  repository = "https://qdrant.to/helm"
  chart      = "qdrant"
  namespace  = var.namespace
  version    = "1.17.0" # pinned for reproducibility; review before bumping
  wait       = true

  # ── Replicas (3 recommended for multi-AZ HA) ─────────────────────────────
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

  set {
    name  = "persistence.storageClassName"
    value = var.qdrant.storage_class
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

  # ── Pod Disruption Budget ────────────────────────────────────────────────
  # Ensures at most 1 pod is unavailable during voluntary disruptions
  # (node drains, cluster upgrades).  Critical for HA with Raft consensus.
  set {
    name  = "podDisruptionBudget.enabled"
    value = true
  }

  set {
    name  = "podDisruptionBudget.maxUnavailable"
    value = 1
  }

  # ── Qdrant config, IRSA, topology, anti-affinity, and probes ─────────────
  values = [yamlencode({
    # IRSA annotation on the chart-managed ServiceAccount
    serviceAccount = {
      annotations = {
        "eks.amazonaws.com/role-arn" = aws_iam_role.qdrant_s3.arn
      }
    }

    # ── Qdrant configuration ───────────────────────────────────────────────
    config = {
      # Distributed cluster mode — mandatory for multi-node deployments.
      # Qdrant uses Raft consensus for metadata and gossip (port 6335)
      # for shard synchronization.
      cluster = {
        enabled = true
        p2p = {
          port = 6335
        }
        consensus = {
          tick_period_ms = 100
        }
      }

      # S3 snapshot storage — for disaster recovery and backup.
      # Primary data lives on EBS PVCs; S3 is for snapshots only.
      # Credentials are provided via IRSA (AWS SDK default chain).
      storage = {
        snapshots_config = {
          snapshots_storage = "s3"
          s3_config = {
            bucket = var.s3_bucket
            region = var.s3_region
          }
        }
      }
    }

    # ── Liveness Probe ─────────────────────────────────────────────────────
    # Detects deadlocked Qdrant pods. Without this, a hung pod stays in the
    # StatefulSet and silently degrades Raft consensus.
    livenessProbe = {
      enabled             = true
      initialDelaySeconds = 10
      periodSeconds       = 10
      timeoutSeconds      = 3
      failureThreshold    = 6
      successThreshold    = 1
    }

    # ── Startup Probe ──────────────────────────────────────────────────────
    # Allows up to 150s (30 * 5s) for initial startup. Prevents the
    # liveness probe from killing slow-starting pods during recovery.
    startupProbe = {
      enabled             = true
      initialDelaySeconds = 10
      periodSeconds       = 5
      timeoutSeconds      = 3
      failureThreshold    = 30
      successThreshold    = 1
    }

    # ── Topology Spread Constraints ────────────────────────────────────────
    # Distribute Qdrant pods evenly across availability zones.
    # Uses the standard Kubernetes topology label:
    #   topology.kubernetes.io/zone
    # DoNotSchedule prevents AZ imbalance — if a zone is full the pod
    # stays Pending rather than stacking onto an already-served zone.
    topologySpreadConstraints = [
      {
        maxSkew           = 1
        topologyKey       = "topology.kubernetes.io/zone"
        whenUnsatisfiable = "DoNotSchedule"
        labelSelector = {
          matchLabels = {
            "app.kubernetes.io/name"     = "qdrant"
            "app.kubernetes.io/instance" = "qdrant"
          }
        }
      }
    ]

    # ── Pod Anti-Affinity ──────────────────────────────────────────────────
    # Prevent two Qdrant pods from landing on the same node.
    # This is complementary to topologySpreadConstraints — TSC handles
    # AZ-level spreading while anti-affinity handles node-level isolation.
    affinity = {
      podAntiAffinity = {
        requiredDuringSchedulingIgnoredDuringExecution = [
          {
            labelSelector = {
              matchExpressions = [
                {
                  key      = "app.kubernetes.io/name"
                  operator = "In"
                  values   = ["qdrant"]
                },
                {
                  key      = "app.kubernetes.io/instance"
                  operator = "In"
                  values   = ["qdrant"]
                },
              ]
            }
            topologyKey = "kubernetes.io/hostname"
          }
        ]
      }
    }
  })]
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 4 – NetworkPolicy: isolate Qdrant p2p and client ports
# ═════════════════════════════════════════════════════════════════════════════
# The Qdrant p2p port (6335) is used for Raft consensus and shard replication.
# It can perform WRITE operations and is unauthenticated — any pod that can
# reach it can join the cluster or mutate data.
#
# This NetworkPolicy restricts:
#   - Port 6335 (p2p): only other Qdrant pods (same StatefulSet)
#   - Ports 6333/6334 (HTTP/gRPC): only pods with label app=genie
# ═════════════════════════════════════════════════════════════════════════════

resource "kubernetes_network_policy" "qdrant" {
  metadata {
    name      = "qdrant-network-policy"
    namespace = var.namespace

    labels = {
      app       = "qdrant"
      ManagedBy = "terraform"
    }
  }

  spec {
    pod_selector {
      match_labels = {
        "app.kubernetes.io/name"     = "qdrant"
        "app.kubernetes.io/instance" = "qdrant"
      }
    }

    # Rule 1: p2p traffic — only other Qdrant pods
    ingress {
      ports {
        port     = "6335"
        protocol = "TCP"
      }

      from {
        pod_selector {
          match_labels = {
            "app.kubernetes.io/name"     = "qdrant"
            "app.kubernetes.io/instance" = "qdrant"
          }
        }
      }
    }

    # Rule 2: HTTP client traffic — only genie pods
    ingress {
      ports {
        port     = "6333"
        protocol = "TCP"
      }

      from {
        pod_selector {
          match_labels = {
            app = "genie"
          }
        }
      }

      # Also allow the snapshot CronJob to reach the HTTP API
      from {
        pod_selector {
          match_labels = {
            app = "qdrant-snapshot"
          }
        }
      }
    }

    # Rule 3: gRPC client traffic — only genie pods
    ingress {
      ports {
        port     = "6334"
        protocol = "TCP"
      }

      from {
        pod_selector {
          match_labels = {
            app = "genie"
          }
        }
      }
    }

    policy_types = ["Ingress"]
  }
}

# ═════════════════════════════════════════════════════════════════════════════
# PART 5 – Snapshot CronJob: automated periodic backups to S3
# ═════════════════════════════════════════════════════════════════════════════
# Qdrant does NOT auto-snapshot — snapshots must be triggered via the REST API.
# This CronJob calls POST /snapshots on each configured collection to create
# full snapshots that are stored in the S3 bucket configured above.
#
# Without this, the S3 snapshot configuration is unused and there are no
# backups to restore from in a disaster recovery scenario.
# ═════════════════════════════════════════════════════════════════════════════

resource "kubernetes_cron_job_v1" "qdrant_snapshot" {
  count = var.snapshot_schedule != "" ? 1 : 0

  metadata {
    name      = "qdrant-snapshot"
    namespace = var.namespace

    labels = {
      app       = "qdrant-snapshot"
      ManagedBy = "terraform"
    }
  }

  spec {
    schedule                      = var.snapshot_schedule
    successful_jobs_history_limit = 3
    failed_jobs_history_limit     = 3
    concurrency_policy            = "Forbid"

    job_template {
      metadata {
        labels = {
          app = "qdrant-snapshot"
        }
      }

      spec {
        backoff_limit = 3

        template {
          metadata {
            labels = {
              app = "qdrant-snapshot"
            }
          }

          spec {
            restart_policy = "OnFailure"

            container {
              name  = "snapshot"
              image = "curlimages/curl:latest"

              command = ["/bin/sh", "-c"]
              args = [<<-EOT
                set -e
                QDRANT_URL="http://qdrant.${var.namespace}.svc.cluster.local:6333"
                echo "[snapshot] Triggering full storage snapshot..."
                RESULT=$(curl -sf -X POST "$QDRANT_URL/snapshots" \
                  -H "Content-Type: application/json" \
                  ${var.qdrant.api_key != "" ? "-H \"api-key: $(QDRANT_API_KEY)\"" : ""})
                echo "[snapshot] Snapshot created: $RESULT"
                echo "[snapshot] Listing snapshots..."
                curl -sf "$QDRANT_URL/snapshots" \
                  ${var.qdrant.api_key != "" ? "-H \"api-key: $(QDRANT_API_KEY)\"" : ""} | head -c 1000
                echo ""
                echo "[snapshot] Done."
              EOT
              ]

              resources {
                requests = {
                  cpu    = "50m"
                  memory = "64Mi"
                }
                limits = {
                  cpu    = "100m"
                  memory = "128Mi"
                }
              }
            }
          }
        }
      }
    }
  }
}
