variable "namespace" {
  description = "Kubernetes namespace for the PostgreSQL deployment"
  type        = string
}

variable "postgres" {
  description = "PostgreSQL configuration"
  type = object({
    storage_size = optional(string, "10Gi")
    image_tag    = optional(string, "16-alpine")
    db_name      = optional(string, "genie")
    db_user      = optional(string, "genie")
    resources_requests = optional(object({
      cpu    = optional(string, "100m")
      memory = optional(string, "256Mi")
    }), {})
    resources_limits = optional(object({
      cpu    = optional(string, "500m")
      memory = optional(string, "512Mi")
    }), {})
  })
  default = {}
}

variable "tags" {
  description = "Tags to apply to AWS resources"
  type        = map(string)
  default     = {}
}
