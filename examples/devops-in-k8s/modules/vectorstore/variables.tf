# ── Variables ───────────────────────────────────────────────────────────────

variable "namespace" {
  description = "Kubernetes namespace where Qdrant will be deployed"
  type        = string
}

variable "s3_bucket" {
  description = "S3 bucket name for Qdrant snapshot storage"
  type        = string
}

variable "s3_region" {
  description = "AWS region for the S3 snapshot bucket"
  type        = string
  default     = "us-east-1"
}

variable "qdrant" {
  description = "Qdrant configuration"
  type = object({
    replicas         = optional(number, 1)
    storage_size     = optional(string, "10Gi")
    image_tag        = optional(string, "")
    api_key          = optional(string, "")
    resources_limits = optional(object({
      cpu    = optional(string, "1")
      memory = optional(string, "2Gi")
    }), {})
    resources_requests = optional(object({
      cpu    = optional(string, "250m")
      memory = optional(string, "512Mi")
    }), {})
  })
  default = {}
}

variable "oidc_provider_arn" {
  description = "ARN of the EKS OIDC provider for IRSA"
  type        = string
}

variable "oidc_issuer" {
  description = "OIDC issuer URL (without https://) for trust policy conditions"
  type        = string
}

variable "tags" {
  description = "Custom tags to apply to all AWS resources"
  type        = map(string)
  default     = {}
}
