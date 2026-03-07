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

variable "auth" {
  description = "Authentication configuration for the AG-UI server"
  type = object({
    password           = optional(string, "")
    oidc_issuer_url    = optional(string, "")
    oidc_client_id     = optional(string, "")
    oidc_client_secret = optional(string, "")
  })
  sensitive = true
  default   = {}
}
