# Omneval load-test infrastructure (AWS)

Provisions everything needed to run `tools/loadtest` against a real
deployment instead of docker-compose: a multi-node EKS cluster, an S3
bucket for the Ingest Buffer + DuckLake data path, and the IAM
roles/policies for S3 access.

This is throwaway infrastructure for benchmarking — `terraform destroy`
tears it all down. **Running this costs real money** (EKS control plane +
3+ worker nodes + NAT gateway, roughly $0.10-$0.15/hr for the control plane
plus on-demand instance costs for the node group — budget a few dollars/hour
total for the default `m5.xlarge` x3 sizing). Destroy it as soon as you're
done.

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
contents, via `force_destroy`), and IAM resources.

## Notes

- `enable_cluster_creator_admin_permissions = true` grants the applying
  IAM identity cluster-admin so `kubectl`/`helm` work immediately — no
  separate aws-auth wiring needed.
- Postgres/Redis are deployed in-cluster via the Helm chart's bundled
  sub-resources for simplicity. If you're publishing absolute throughput
  numbers, consider managed RDS/ElastiCache instead so in-cluster storage
  I/O isn't a confound.
- `k8s_service_accounts` in `variables.tf` assumes a Helm release named
  `omneval`; adjust if you install under a different release name.
