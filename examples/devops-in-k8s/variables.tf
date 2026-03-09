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
    port     = optional(number, 9876)
  })
  default = {}
}

variable "kubernetes" {
  description = "Kubernetes deployment configuration"
  type = object({
    namespace        = optional(string, "default")
    create_namespace = optional(bool, false)
    ingress_host     = optional(string, "genie.dev.stackgen.com")
  })
  default = {}
}

variable "vectorstore" {
  description = "Qdrant vector store configuration"
  type = object({
    s3_bucket = string
    replicas  = optional(number, 1)
    storage_size = optional(string, "10Gi")
    image_tag    = optional(string, "")
    api_key      = optional(string, "")
    resources_limits = optional(object({
      cpu    = optional(string, "1")
      memory = optional(string, "2Gi")
    }), {})
    resources_requests = optional(object({
      cpu    = optional(string, "250m")
      memory = optional(string, "512Mi")
    }), {})
  })
}

variable "tags" {
  description = "Custom tags to apply to all resources"
  type        = map(string)
  default     = {}
}
