#!/usr/bin/env bash
# ============================================================================
# teardown_benchmark.sh — Tear down the Omneval benchmark environment
#
# Removes the Helm release, all pods, PVCs, and associated Kubernetes
# resources.  S3 bucket data is NOT automatically deleted — you must
# manually clear (or delete) the S3 bucket to avoid ongoing storage costs.
#
# This script does NOT touch the S3 bucket itself because:
#   1. The bucket may contain data you want to keep
#   2. The bucket may be shared with other projects
#   3. Cloud provider IAM policy may restrict bucket deletion to a different
#      account than the one running this script
#
# You SHOULD manually delete the S3 bucket when you're done with the
# benchmark environment to avoid ongoing storage costs.
#
# Usage:
#   ./teardown_benchmark.sh
#
# Environment variables:
#   OMNEVAL_NAMESPACE — Kubernetes namespace (default: omneval-benchmark)
#   OMNEVAL_RELEASE   — Helm release name  (default: omneval-benchmark)
#
# See README.md for cost considerations and S3 bucket cleanup steps.
# ============================================================================

set -euo pipefail

NAMESPACE="${OMNEVAL_NAMESPACE:-omneval-benchmark}"
RELEASE="${OMNEVAL_RELEASE:-omneval-benchmark}"

echo "=== Omneval Benchmark Environment — Teardown ==="
echo ""
echo "  Namespace:  $NAMESPACE"
echo "  Release:    $RELEASE"
echo ""
echo "WARNING: This will remove the Helm release, all pods, PVCs, and"
echo "associated Kubernetes resources in namespace '$NAMESPACE'."
echo ""
echo "The S3 bucket '$OMNEVAL_S3_BUCKET' will NOT be deleted automatically."
echo "You must manually delete it to avoid ongoing storage costs."
echo ""

# Confirm teardown if not in CI/non-interactive mode
if [[ -t 0 ]]; then
  read -rp "Proceed with teardown? [y/N] " confirm
  if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
    echo "Aborted."
    exit 0
  fi
fi

echo ""
echo "--- Removing Helm release ---"
helm uninstall "$RELEASE" --namespace "$NAMESPACE" 2>/dev/null || {
  echo "Helm release '$RELEASE' not found (already removed or never deployed)."
}

echo ""
echo "--- Deleting namespace $NAMESPACE (cascading cleanup) ---"
# The namespace deletion cascades to all owned resources (PVCs, services,
# configmaps, secrets, etc.). If the namespace was never created, skip.
if kubectl get namespace "$NAMESPACE" &>/dev/null; then
  kubectl delete namespace "$NAMESPACE" --wait=true || true
  echo "Namespace '$NAMESPACE' deleted."
else
  echo "Namespace '$NAMESPACE' does not exist (already deleted or never created)."
fi

echo ""
echo "=== Teardown complete ==="
echo ""
echo "IMPORTANT: Manually delete your S3 bucket to avoid ongoing costs:"
echo ""
echo "  AWS CLI:"
echo "    aws s3 rb s3://$(echo "$OMNEVAL_S3_BUCKET" || echo '<your-bucket>') --force"
echo ""
echo "  Cloudflare R2 (via rclone or the R2 console):"
echo "    rclone rmdir remote:$(echo "$OMNEVAL_S3_BUCKET" || echo '<your-bucket>')"
echo ""
echo "  Or via the cloud provider's web console."
echo ""
echo "S3 lifecycle policies: if you configured a lifecycle rule to delete"
echo "objects after a certain period, verify it was active during the"
echo "benchmark's lifetime."