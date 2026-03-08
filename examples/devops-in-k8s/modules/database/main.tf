# ═════════════════════════════════════════════════════════════════════════════
# PostgreSQL Database Module
# ═════════════════════════════════════════════════════════════════════════════
# Deploys a single-replica PostgreSQL StatefulSet in the given namespace.
# The password is auto-generated and stored in a Kubernetes Secret.
# A DSN output is provided for the Genie application to connect.
# ═════════════════════════════════════════════════════════════════════════════

terraform {
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.25"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.0"
    }
  }
}

# ── Random password for the database ────────────────────────────────────────

resource "random_password" "postgres" {
  length  = 32
  special = false # avoid URL-encoding issues in DSN
}

# ── Kubernetes Secret (stores credentials) ──────────────────────────────────

resource "kubernetes_secret" "postgres" {
  metadata {
    name      = "postgres-credentials"
    namespace = var.namespace

    labels = {
      app = "postgres"
    }
  }

  data = {
    POSTGRES_DB       = var.postgres.db_name
    POSTGRES_USER     = var.postgres.db_user
    POSTGRES_PASSWORD = random_password.postgres.result
    # Full DSN for application consumption
    POSTGRES_DSN = "postgres://${var.postgres.db_user}:${random_password.postgres.result}@postgres.${var.namespace}.svc.cluster.local:5432/${var.postgres.db_name}?sslmode=disable"
  }
}

# ── PVC for PostgreSQL data ─────────────────────────────────────────────────

resource "kubernetes_persistent_volume_claim" "postgres" {
  metadata {
    name      = "postgres-data"
    namespace = var.namespace

    labels = {
      app = "postgres"
    }
  }

  spec {
    access_modes = ["ReadWriteOnce"]

    resources {
      requests = {
        storage = var.postgres.storage_size
      }
    }
  }
}

# ── StatefulSet ─────────────────────────────────────────────────────────────

resource "kubernetes_stateful_set" "postgres" {
  metadata {
    name      = "postgres"
    namespace = var.namespace

    labels = {
      app = "postgres"
    }
  }

  spec {
    service_name = "postgres"
    replicas     = 1

    selector {
      match_labels = {
        app = "postgres"
      }
    }

    template {
      metadata {
        labels = {
          app = "postgres"
        }
      }

      spec {
        container {
          name  = "postgres"
          image = "postgres:${var.postgres.image_tag}"

          port {
            container_port = 5432
            name           = "postgres"
          }

          env_from {
            secret_ref {
              name = kubernetes_secret.postgres.metadata[0].name
            }
          }

          volume_mount {
            name       = "postgres-data"
            mount_path = "/var/lib/postgresql/data"
            sub_path   = "pgdata"
          }

          resources {
            requests = {
              cpu    = var.postgres.resources_requests.cpu
              memory = var.postgres.resources_requests.memory
            }

            limits = {
              cpu    = var.postgres.resources_limits.cpu
              memory = var.postgres.resources_limits.memory
            }
          }

          liveness_probe {
            exec {
              command = ["pg_isready", "-U", var.postgres.db_user]
            }

            initial_delay_seconds = 30
            period_seconds        = 10
            timeout_seconds       = 5
            failure_threshold     = 6
          }

          readiness_probe {
            exec {
              command = ["pg_isready", "-U", var.postgres.db_user]
            }

            initial_delay_seconds = 5
            period_seconds        = 10
            timeout_seconds       = 5
          }
        }

        volume {
          name = "postgres-data"

          persistent_volume_claim {
            claim_name = kubernetes_persistent_volume_claim.postgres.metadata[0].name
          }
        }
      }
    }
  }
}

# ── Service ─────────────────────────────────────────────────────────────────

resource "kubernetes_service" "postgres" {
  metadata {
    name      = "postgres"
    namespace = var.namespace

    labels = {
      app = "postgres"
    }
  }

  spec {
    selector = {
      app = "postgres"
    }

    port {
      port        = 5432
      target_port = 5432
      name        = "postgres"
    }
  }
}
