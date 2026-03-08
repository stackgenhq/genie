# ── Variables ───────────────────────────────────────────────────────────────

variable "aws" {
  description = "AWS Configuration"
  type = object({
    region              = optional(string, "us-east-1")
    eks_cluster_name    = string
    secrets_manager_arn = string
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

variable "tags" {
  description = "Custom tags to apply to all resources"
  type        = map(string)
  default     = {}
}
