#!/usr/bin/env bash
# ============================================================================
# deploy_benchmark.sh — Stand up the Omneval benchmark environment
#
# Deploys a single-node, real-S3 benchmark environment to a Kubernetes
# cluster using Helm.  The environment is idempotent: running this script
# twice (after a teardown) produces an equivalent environment with no
# manual steps.
#
# Prerequisites:
#   - kubectl configured with access to a Kubernetes cluster
#   - helm v3.x installed
#   - a real S3 bucket and credentials (see README.md)
#
# Usage:
#   ./deploy_benchmark.sh
#
# The script requires you to set the following environment variables
# (or pass them as CLI arguments):
#   OMNEVAL_S3_BUCKET      — S3 bucket name (e.g. "my-omneval-bucket")
#   OMNEVAL_S3_ENDPOINT    — S3 endpoint URL (e.g. "https://s3.us-east-1.amazonaws.com")
#   OMNEVAL_S3_ACCESS_KEY  — S3 access key ID
#   OMNEVAL_S3_SECRET_KEY  — S3 secret access key
#   OMNEVAL_IMAGE_TAG      — Docker image tag (default: latest from local build)
#   OMNEVAL_NAMESPACE      — Kubernetes namespace (default: omneval-benchmark)
#   OMNEVAL_RELEASE        — Helm release name  (default: omneval-benchmark)
#
# Teardown: see teardown_benchmark.sh
#
# See README.md for full documentation, node-shape rationale, and
# how to point the benchmark harness at the resulting endpoints.
# ============================================================================

set -euo pipefail

# ── Defaults ────────────────────────────────────────────────────────────────
NAMESPACE="${OMNEVAL_NAMESPACE:-omneval-benchmark}"
RELEASE="${OMNEVAL_RELEASE:-omneval-benchmark}"
IMAGE_TAG="${OMNEVAL_IMAGE_TAG:-latest}"

# ── Required vars (from environment or CLI args) ───────────────────────────
S3_BUCKET="${OMNEVAL_S3_BUCKET:?OMNEVAL_S3_BUCKET is required.  Export it or pass as a CLI argument.}"
S3_ENDPOINT="${OMNEVAL_S3_ENDPOINT:?OMNEVAL_S3_ENDPOINT is required.  Export it or pass as a CLI argument.}"
S3_ACCESS_KEY="${OMNEVAL_S3_ACCESS_KEY:?OMNEVAL_S3_ACCESS_KEY is required.  Export it or pass as a CLI argument.}"
S3_SECRET_KEY="${OMNEVAL_S3_SECRET_KEY:?OMNEVAL_S3_SECRET_KEY is required.  Export it or pass as a CLI argument.}"

# Parse CLI args (override env vars)
while [[ $# -gt 0 ]]; do
  case "$1" in
    --bucket)    S3_BUCKET="$2";      shift 2 ;;
    --endpoint)  S3_ENDPOINT="$2";    shift 2 ;;
    --access-key) S3_ACCESS_KEY="$2"; shift 2 ;;
    --secret-key) S3_SECRET_KEY="$2"; shift 2 ;;
    --image-tag) IMAGE_TAG="$2";      shift 2 ;;
    --namespace) NAMESPACE="$2";      shift 2 ;;
    --release)   RELEASE="$2";        shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
HELM_DIR="$REPO_ROOT/deploy/helm"

# ── Validation ──────────────────────────────────────────────────────────────
echo "=== Omneval Benchmark Environment — Deployment ==="
echo ""
echo "  Namespace:  $NAMESPACE"
echo "  Release:    $RELEASE"
echo "  Image tag:  $IMAGE_TAG"
echo "  S3 bucket:  $S3_BUCKET"
echo "  S3 endpoint: $S3_ENDPOINT"
echo ""

# Verify prerequisites
for cmd in kubectl helm; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "ERROR: '$cmd' is not installed. Please install it first."
    exit 1
  fi
done

# Verify kubectl connectivity
if ! kubectl cluster-info &>/dev/null; then
  echo "ERROR: Cannot reach Kubernetes cluster. Check your kubeconfig."
  exit 1
fi

# ── Create namespace ───────────────────────────────────────────────────────
echo ""
echo "--- Creating namespace $NAMESPACE ---"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

# ── Build Docker image (if needed) ─────────────────────────────────────────
# Check if the image exists locally; build it if not.
echo ""
echo "--- Ensuring Docker image is available ---"
if ! docker image inspect "${IMAGE_TAG:+omneval/app:${IMAGE_TAG}}" &>/dev/null 2>&1; then
  echo "Image omneval/app:${IMAGE_TAG} not found locally — building..."
  docker build -t "omneval/app:${IMAGE_TAG}" "$REPO_ROOT"
fi

# ── Deploy with Helm (idempotent) ──────────────────────────────────────────
echo ""
echo "--- Running helm upgrade --install ---"
helm upgrade --install "$RELEASE" "$HELM_DIR" \
  --values "$SCRIPT_DIR/values-benchmark.yaml" \
  --namespace "$NAMESPACE" \
  --create-namespace \
  --set-string image.tag="${IMAGE_TAG}" \
  --set-string storage.bucket="${S3_BUCKET}" \
  --set-string storage.endpoint="${S3_ENDPOINT}" \
  --set-string storage.accessKey="${S3_ACCESS_KEY}" \
  --set-string storage.secretKey="${S3_SECRET_KEY}"

# ── Wait for pods to be ready ─────────────────────────────────────────────
echo ""
echo "--- Waiting for pods to become ready ---"
kubectl wait --namespace "$NAMESPACE" \
  --for=condition=Ready --timeout=300s \
  pods --selector=app.kubernetes.io/instance="$RELEASE" \
  || {
    echo ""
    echo "Pods did not become ready within 5 minutes. Here is the current state:"
    kubectl get pods --namespace "$NAMESPACE" --selector=app.kubernetes.io/instance="$RELEASE" -o wide
    echo ""
    echo "Events:"
    kubectl get events --namespace "$NAMESPACE" --sort-by='.lastTimestamp' | tail -20
    exit 1
  }

# ── Summary ─────────────────────────────────────────────────────────────────
echo ""
echo "=== Deployment complete ==="
echo ""

INGEST_URL="http://localhost:8000"
QUERY_URL="http://localhost:8002"

echo "  Ingest API:   $INGEST_URL"
echo "  Query API:    $QUERY_URL"
echo "  Quack Server: internal service $RELEASE-quack-server:9494"
echo "  Metrics:      internal service on port 9090 per pod"
echo ""
echo "Port-forward for local access:"
echo "  kubectl port-forward --namespace $NAMESPACE svc/${RELEASE}-ingest 8000:8000 &"
echo "  kubectl port-forward --namespace $NAMESPACE svc/${RELEASE}-query 8002:8002 &"
echo ""
echo "To view all services:"
echo "  kubectl get svc --namespace $NAMESPACE -l app.kubernetes.io/instance=$RELEASE"
echo ""
echo "Teardown: ./teardown_benchmark.sh"
echo "Documentation: README.md"