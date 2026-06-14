# RDS Postgres — shared by the Omneval metadata store and the DuckLake
# Catalog (the same Postgres instance backs both, per
# docs/adr/0004-ducklake-storage-core.md). Replacing in-cluster Postgres with
# RDS removes in-cluster storage I/O as a confound when benchmarking the
# Quack Server's per-commit catalog-transaction overhead.

resource "random_password" "postgres" {
  length  = 24
  special = false
}

resource "aws_db_subnet_group" "postgres" {
  name       = "${var.name}-postgres"
  subnet_ids = module.vpc.private_subnets
}

resource "aws_security_group" "postgres" {
  name        = "${var.name}-postgres"
  description = "Allow Postgres access from the EKS worker nodes."
  vpc_id      = module.vpc.vpc_id

  ingress {
    description     = "Postgres from EKS nodes"
    from_port       = 5432
    to_port         = 5432
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

resource "aws_db_instance" "postgres" {
  identifier     = "${var.name}-postgres"
  engine         = "postgres"
  engine_version = var.db_engine_version
  instance_class = var.db_instance_class

  allocated_storage = var.db_allocated_storage_gb
  storage_type      = "gp3"
  storage_encrypted = true

  db_name  = "omneval"
  username = "omneval"
  password = random_password.postgres.result
  port     = 5432

  db_subnet_group_name   = aws_db_subnet_group.postgres.name
  vpc_security_group_ids = [aws_security_group.postgres.id]
  publicly_accessible    = false
  multi_az               = false

  skip_final_snapshot = true
  deletion_protection = false

  # Avoid auto-upgrade churn during a load test.
  auto_minor_version_upgrade = false
  apply_immediately          = true
}
