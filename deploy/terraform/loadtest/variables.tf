variable "region" {
  description = "AWS region to deploy into."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "Name prefix for all resources (cluster, VPC, S3 bucket, IAM)."
  type        = string
  default     = "omneval-loadtest"
}

variable "kubernetes_version" {
  description = "EKS Kubernetes version."
  type        = string
  default     = "1.31"
}

variable "vpc_cidr" {
  description = "CIDR block for the load test VPC."
  type        = string
  default     = "10.42.0.0/16"
}

variable "availability_zone_count" {
  description = "Number of AZs to spread the VPC/EKS node group across."
  type        = number
  default     = 2
}

variable "node_instance_types" {
  description = "Instance types for the EKS managed node group."
  type        = list(string)
  default     = ["m5.xlarge"]
}

variable "node_desired_size" {
  description = "Desired number of worker nodes. Sized so Quack (1 replica), Postgres, Redis, and several Writer/Ingest replicas can be scheduled without contention."
  type        = number
  default     = 3
}

variable "node_min_size" {
  description = "Minimum number of worker nodes."
  type        = number
  default     = 3
}

variable "node_max_size" {
  description = "Maximum number of worker nodes (for scaling Writer/Ingest replicas during the test)."
  type        = number
  default     = 6
}

variable "node_disk_size_gb" {
  description = "Root EBS volume size (GB) for worker nodes."
  type        = number
  default     = 100
}

variable "k8s_namespace" {
  description = "Kubernetes namespace the Omneval Helm release is installed into. Used for IRSA trust policy conditions."
  type        = string
  default     = "omneval"
}

variable "db_instance_class" {
  description = "RDS instance class for the shared Postgres (Omneval metadata store + DuckLake Catalog)."
  type        = string
  default     = "db.m5.large"
}

variable "db_engine_version" {
  description = "RDS PostgreSQL engine version."
  type        = string
  default     = "16"
}

variable "db_allocated_storage_gb" {
  description = "RDS allocated storage (GB), gp3."
  type        = number
  default     = 50
}

variable "redis_node_type" {
  description = "ElastiCache node type for the Omneval ingest queue (Redis)."
  type        = string
  default     = "cache.m5.large"
}

variable "redis_engine_version" {
  description = "ElastiCache Redis engine version."
  type        = string
  default     = "7.1"
}

variable "k8s_service_accounts" {
  description = <<-EOT
    Kubernetes ServiceAccount names allowed to assume the S3 access IRSA
    role, e.g. "<helm-release-name>-ingest". Adjust to match the names the
    Helm chart generates for your release (see deploy/helm/values.yaml,
    *.serviceAccount.create / name). Defaults assume a release named
    "omneval".
  EOT
  type        = list(string)
  default = [
    "omneval-ingest",
    "omneval-writer",
    "omneval-query",
    "omneval-eval",
    "omneval-quack-server",
  ]
}
