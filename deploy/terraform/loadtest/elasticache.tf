# ElastiCache Redis — backs the Omneval Ingest -> Writer queue (Batch ID
# references). Replacing in-cluster Redis with ElastiCache removes
# in-cluster queue latency/throughput as a confound when benchmarking the
# Ingest -> Writer handoff.
#
# transit_encryption_enabled = false because the Omneval Redis client
# (services/ingest, services/eval) is constructed with redis.Options{} and
# does not currently configure TLS. AUTH tokens require transit encryption
# on Redis 6+, so auth is left disabled here to match.

resource "aws_elasticache_subnet_group" "redis" {
  name       = "${var.name}-redis"
  subnet_ids = module.vpc.private_subnets
}

resource "aws_security_group" "redis" {
  name        = "${var.name}-redis"
  description = "Allow Redis access from the EKS worker nodes."
  vpc_id      = module.vpc.vpc_id

  ingress {
    description     = "Redis from EKS nodes"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [module.eks.node_security_group_id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_elasticache_replication_group" "redis" {
  replication_group_id = "${var.name}-redis"
  description          = "Omneval load-test ingest queue."

  engine         = "redis"
  engine_version = var.redis_engine_version
  node_type      = var.redis_node_type
  port           = 6379

  num_cache_clusters = 1

  subnet_group_name  = aws_elasticache_subnet_group.redis.name
  security_group_ids = [aws_security_group.redis.id]

  at_rest_encryption_enabled = true
  transit_encryption_enabled = false
}
