# ── Variables ───────────────────────────────────────────────────────────────

variable "aws" {
  description = "AWS Configuration"
  type = object({
    region                         = optional(string, "us-east-1")
    eks_cluster_name               = string
    secrets_manager_arn            = string
    secrets_manager_name           = string
    gdrive_credentials_secret_path = optional(string, "") # Secrets Manager name containing GDRIVE_SA_JSON; empty = GDrive SA disabled
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

variable "messenger" {
  description = "Messenger access control"
  type = object({
    allowed_senders = optional(list(string), [])
  })
  default = {}
}

variable "data_sources" {
  description = "Data sources configuration for RAG vectorization"
  type = object({
    gdrive_folder_ids = optional(list(string), [])
    slack_channel_ids = optional(list(string), [])
  })
  default = {}
}

variable "tags" {
  description = "Custom tags to apply to all resources"
  type        = map(string)
  default     = {}
}
