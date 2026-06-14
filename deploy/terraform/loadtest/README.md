# Omneval load-test infrastructure (AWS)

Provisions everything needed to run `tools/loadtest` against a real
deployment instead of docker-compose: a multi-node EKS cluster, an S3
bucket for the Ingest Buffer + DuckLake data path, an RDS Postgres instance
(metadata store + DuckLake Catalog), an ElastiCache Redis instance (ingest
queue), and the IAM roles/policies for S3 access.

This is throwaway infrastructure for benchmarking — `terraform destroy`
tears it all down. **Running this costs real money.** Budget roughly
$1.50-$2.00/hr total for the defaults: EKS control plane (~$0.10/hr) + 3x
`m5.xlarge` worker nodes (~$0.51/hr) + NAT gateway (~$0.05/hr + data) +
`db.m5.large` RDS Postgres (~$0.18/hr + ~$0.06/hr gp3 storage) +
`cache.m5.large` ElastiCache Redis, single node (~$0.16/hr). Destroy it as
soon as you're done.

## What this creates

- A dedicated VPC (2 AZs, private subnets for nodes, single NAT gateway)
- An EKS cluster (default: Kubernetes 1.31, 3x `m5.xlarge` managed nodes,
  scalable to 6)
- An S3 bucket (`force_destroy = true`) for `storage.bucket`
- An IAM user + access key with read/write access to that bucket, matching
  the static-credential model the Omneval S3 client currently expects
  (`storage.accessKey` / `storage.secretKey` in `deploy/helm/values.yaml`)
- An IRSA role for the same bucket, pre-wired to the cluster's OIDC
  provider for forward compatibility (see the note in `iam.tf` — the
  current S3 client doesn't use the AWS default credential chain yet, so
  this role isn't consumed by the app today)
- An RDS Postgres instance (default: `db.m5.large`, 50GB gp3), in the VPC's
  private subnets, reachable only from the EKS worker nodes — used as both
  the Omneval metadata store and the DuckLake Catalog
  (`postgresql.external.*`)
- An ElastiCache Redis replication group (default: `cache.m5.large`, single
  node), in the VPC's private subnets, reachable only from the EKS worker
  nodes — used for the Ingest -> Writer queue (`redis.external.addr`)

## Usage

### 1. Apply

```bash
cd deploy/terraform/loadtest
terraform init
terraform apply
```

Review the plan — this creates billable resources. Defaults are in
`variables.tf` (region, instance types, node counts); override with
`-var` or a `*.tfvars` file as needed.

### 2. Configure kubectl and deploy Omneval

```bash
$(terraform output -raw configure_kubectl)

cp values-loadtest.example.yaml values-loadtest.yaml
# Fill in storage.* from:
terraform output s3_bucket
terraform output s3_endpoint
terraform output omneval_s3_access_key_id
terraform output -raw omneval_s3_secret_access_key

# Fill in postgresql.external.* from:
terraform output rds_host
terraform output rds_port
terraform output rds_database
terraform output rds_username
terraform output -raw rds_password

# Fill in redis.external.addr from:
terraform output redis_addr

helm install omneval ../../helm -f values-loadtest.yaml -n omneval --create-namespace
```

Wait for all pods to be ready, then port-forward or expose the Ingest API
and Writer metrics endpoint (or use a LoadBalancer Service / Ingress —
not provisioned here to keep this minimal).

### 3. Run the load test

Follow `tools/loadtest/README.md` to create a project + API key via the
Query API, then:

```bash
cd ../../../tools/loadtest
GOWORK=off go run . \
  -url http://<ingest-lb-or-port-forward>:8000 \
  -api-key "$API_KEY" \
  -duration 5m \
  -concurrency 50 \
  -batch-size 50 \
  -payload-bytes 500 \
  -writer-metrics-url http://<writer-lb-or-port-forward>:9091/metrics
```

Vary `-rate`, `-concurrency`, `-payload-bytes`, and `writer.replicas` /
`quack.server` resources between runs per the methodology discussion —
keep node sizing/resource limits fixed and documented for each reported
number.

### 4. Tear down

```bash
helm uninstall omneval -n omneval
cd deploy/terraform/loadtest
terraform destroy
```

`terraform destroy` removes the cluster, VPC, S3 bucket (and its
contents, via `force_destroy`), RDS instance, ElastiCache replication
group, and IAM resources.

## Notes

- `enable_cluster_creator_admin_permissions = true` grants the applying
  IAM identity cluster-admin so `kubectl`/`helm` work immediately — no
  separate aws-auth wiring needed.
- RDS and ElastiCache are sized via `db_instance_class`/`db_allocated_storage_gb`
  and `redis_node_type`/`redis_engine_version` in `variables.tf`. The
  ElastiCache replication group runs `transit_encryption_enabled = false`
  (no AUTH token) because the Omneval Redis client doesn't currently
  configure TLS; it's only reachable from the EKS node security group.
- `k8s_service_accounts` in `variables.tf` assumes a Helm release named
  `omneval`; adjust if you install under a different release name.
