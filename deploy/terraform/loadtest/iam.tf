# --- S3 access policy, shared by the IRSA role and the static-credential IAM user ---

data "aws_iam_policy_document" "s3_access" {
  statement {
    sid    = "ListBucket"
    effect = "Allow"
    actions = [
      "s3:ListBucket",
      "s3:GetBucketLocation",
    ]
    resources = [aws_s3_bucket.lake.arn]
  }

  statement {
    sid    = "ReadWriteObjects"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
    ]
    resources = ["${aws_s3_bucket.lake.arn}/*"]
  }
}

resource "aws_iam_policy" "s3_access" {
  name        = "${var.name}-s3-access"
  description = "Read/write access to the Omneval load-test S3 bucket (Ingest Buffer + DuckLake data path)."
  policy      = data.aws_iam_policy_document.s3_access.json
}

# --- IRSA role for in-cluster service accounts ---
#
# NOTE: as of this writing, the Omneval services build their S3 client with
# static credentials (minio-go credentials.NewStaticV4 — see
# internal/storage/s3/s3.go) rather than the AWS SDK default credential
# chain, so this role is not yet picked up automatically by pod IAM tokens.
# It's provided for forward compatibility (once the S3 client supports the
# default credential chain / web identity tokens) and so cluster-admin tools
# (e.g. aws-cli sidecars, S3 browsers) can use it today. For the Omneval
# services themselves, use the `omneval_s3_access_key` output below to
# populate storage.accessKey/storage.secretKey.

module "irsa_s3_access" {
  source  = "terraform-aws-modules/iam/aws//modules/iam-role-for-service-accounts-eks"
  version = "~> 5.48"

  role_name = "${var.name}-s3-access"

  oidc_providers = {
    main = {
      provider_arn = module.eks.oidc_provider_arn
      namespace_service_accounts = [
        for sa in var.k8s_service_accounts : "${var.k8s_namespace}:${sa}"
      ]
    }
  }
}

resource "aws_iam_role_policy_attachment" "irsa_s3_access" {
  role       = module.irsa_s3_access.iam_role_name
  policy_arn = aws_iam_policy.s3_access.arn
}

# --- Static-credential IAM user ---
#
# Matches the credential model the Omneval services currently expect
# (storage.accessKey / storage.secretKey in deploy/helm/values.yaml).

resource "aws_iam_user" "s3_access" {
  name = "${var.name}-s3-access"
}

resource "aws_iam_user_policy_attachment" "s3_access" {
  user       = aws_iam_user.s3_access.name
  policy_arn = aws_iam_policy.s3_access.arn
}

resource "aws_iam_access_key" "s3_access" {
  user = aws_iam_user.s3_access.name
}
