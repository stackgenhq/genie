# ── Outputs ─────────────────────────────────────────────────────────────────

output "iam_role_arn" {
  description = "ARN of the IAM role assigned to the Qdrant service account"
  value       = aws_iam_role.qdrant_s3.arn
}

output "service_account_name" {
  description = "Name of the Kubernetes service account used by Qdrant (managed by Helm)"
  value       = "qdrant"
}

output "grpc_endpoint" {
  description = "Internal cluster gRPC endpoint for Qdrant (port 6334)"
  value       = "qdrant.${var.namespace}.svc.cluster.local:6334"
}

output "http_endpoint" {
  description = "Internal cluster HTTP endpoint for Qdrant (port 6333)"
  value       = "qdrant.${var.namespace}.svc.cluster.local:6333"
}
