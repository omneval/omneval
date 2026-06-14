# S3 bucket backing both the Ingest Buffer and the DuckLake data path
# (ADR-0004). force_destroy is enabled since this bucket only holds
# disposable load-test data.
resource "aws_s3_bucket" "lake" {
  bucket        = "${var.name}-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "lake" {
  bucket = aws_s3_bucket.lake.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

data "aws_caller_identity" "current" {}
