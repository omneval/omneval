output "cluster_name" {
  description = "EKS cluster name."
  value       = module.eks.cluster_name
}

output "configure_kubectl" {
  description = "Command to configure kubectl/helm for this cluster."
  value       = "aws eks update-kubeconfig --region ${var.region} --name ${module.eks.cluster_name}"
}

output "s3_bucket" {
  description = "S3 bucket backing the Ingest Buffer and DuckLake data path."
  value       = aws_s3_bucket.lake.bucket
}

output "s3_endpoint" {
  description = "S3 endpoint for storage.endpoint in deploy/helm/values.yaml."
  value       = "https://s3.${var.region}.amazonaws.com"
}

output "irsa_role_arn" {
  description = "IAM role ARN for IRSA-annotated service accounts (see iam.tf for caveats)."
  value       = module.irsa_s3_access.iam_role_arn
}

output "omneval_s3_access_key_id" {
  description = "Access key ID for storage.accessKey (static-credential path used by the current S3 client)."
  value       = aws_iam_access_key.s3_access.id
}

output "omneval_s3_secret_access_key" {
  description = "Secret access key for storage.secretKey."
  value       = aws_iam_access_key.s3_access.secret
  sensitive   = true
}

output "rds_host" {
  description = "RDS Postgres endpoint hostname, for postgresql.external.host (metadata store + DuckLake Catalog)."
  value       = aws_db_instance.postgres.address
}

output "rds_port" {
  description = "RDS Postgres port, for postgresql.external.port."
  value       = aws_db_instance.postgres.port
}

output "rds_database" {
  description = "RDS Postgres database name, for postgresql.external.database."
  value       = aws_db_instance.postgres.db_name
}

output "rds_username" {
  description = "RDS Postgres username, for postgresql.external.user."
  value       = aws_db_instance.postgres.username
}

output "rds_password" {
  description = "RDS Postgres password, for postgresql.external.password."
  value       = random_password.postgres.result
  sensitive   = true
}

output "redis_addr" {
  description = "ElastiCache Redis primary endpoint address:port, for redis.external.addr."
  value       = "${aws_elasticache_replication_group.redis.primary_endpoint_address}:${aws_elasticache_replication_group.redis.port}"
}
