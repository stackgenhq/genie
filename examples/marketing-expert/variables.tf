# ── Variables ───────────────────────────────────────────────────────────────

variable "aws" {
  description = "AWS Configuration"
  type = object({
    region               = optional(string, "us-east-1")
    eks_cluster_name     = string
    secrets_manager_arn  = string
    secrets_manager_name = string
  })
}

variable "genie" {
  description = "Genie configuration"
  type = object({
    image    = optional(string, "ghcr.io/stackgenhq/genie:latest")
    replicas = optional(number, 1)
    port     = optional(number, 9877) # Different port from devops-copilot (9876)
  })
  default = {}
}

variable "kubernetes" {
  description = "Kubernetes deployment configuration"
  type = object({
    namespace        = optional(string, "marketing")
    create_namespace = optional(bool, false)
    ingress_host     = optional(string, "marketing.dev.stackgen.com")
  })
  default = {}
}

variable "qdrant" {
  description = "Qdrant vector store connection (shared with devops-copilot)"
  type = object({
    host    = string
    port    = number
    api_key = optional(string, "")
  })
}

variable "postgres" {
  description = "PostgreSQL database configuration (reuses existing PostgreSQL instance)"
  type = object({
    db_name = optional(string, "genie_marketing")
  })
  default = {}
}

variable "tags" {
  description = "Custom tags to apply to all resources"
  type        = map(string)
  default     = {}
}
