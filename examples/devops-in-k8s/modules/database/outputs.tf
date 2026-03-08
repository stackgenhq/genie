output "dsn" {
  description = "PostgreSQL DSN for application connection"
  value       = "postgres://${var.postgres.db_user}:${random_password.postgres.result}@postgres.${var.namespace}.svc.cluster.local:5432/${var.postgres.db_name}?sslmode=disable"
  sensitive   = true
}

output "host" {
  description = "PostgreSQL service hostname"
  value       = "postgres.${var.namespace}.svc.cluster.local"
}

output "port" {
  description = "PostgreSQL service port"
  value       = 5432
}

output "secret_name" {
  description = "Name of the Kubernetes Secret containing PostgreSQL credentials"
  value       = kubernetes_secret.postgres.metadata[0].name
}
