#!/usr/bin/env bash
# post-cutover-validation.sh — Post-Cutover Validation Checklist
#
# Usage:
#   ./post-cutover-validation.sh <release> <namespace>
#
# This script automates the validation checks from the Catalog cutover runbook
# (docs/runbooks/catalog-cutover.md, Phase 6). It verifies:
#   1. Quack Server catalog_driver = duckdb
#   2. Quack Server backup.enabled = true
#   3. Quack Server PVC exists and catalog file path is accessible
#   4. Old Postgres Catalog config removed from deployment config
#   5. Quack Server is healthy and running
#   6. Writer pods are running (after resume)
#
# After this script, manually execute the three validation checks from the
# runbook:
#   - Check 1: Writer commit (ingest a span, verify it appears in Lake)
#   - Check 2: Eval score write-back (write a score, verify it appears)
#   - Check 3: Query read all (read back all spans, verify counts match)

set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "Usage: $0 <release> <namespace>"
  echo "  release: Helm release name (default: omneval)"
  echo "  namespace: Kubernetes namespace (default: omneval)"
  exit 1
fi

RELEASE="${1:-omneval}"
NAMESPACE="${2:-omneval}"

PASSED=0
FAILED=0
MANUAL=0
MANUAL_HINT=""

pass() { echo "  ✅ $1"; PASSED=$((PASSED + 1)); }
fail() { echo "  ❌ $1"; FAILED=$((FAILED + 1)); }
manual() { echo "  ⚠️  MANUAL: $1"; MANUAL=$((MANUAL + 1)); }

echo "================================================================"
echo "Post-Cutover Validation — ${RELEASE} in ${NAMESPACE}"
echo "================================================================"

# Check 1: Quack Server catalog_driver = duckdb
echo ""
echo "1. Checking catalog_driver..."
CATALOG_DRIVER=$(kubectl get configmap "${RELEASE}-config" -n "${NAMESPACE}" -o jsonpath='{.data.omneval\.yaml}' 2>/dev/null | grep catalog_driver | awk '{print $2}' || true)
if [[ "${CATALOG_DRIVER}" == "duckdb" ]]; then
  pass "catalog_driver = duckdb"
else
  fail "catalog_driver = ${CATALOG_DRIVER:-<unset>} (expected: duckdb)"
fi

# Check 2: Quack Server backup.enabled = true
echo ""
echo "2. Checking backup.enabled..."
BACKUP_ENABLED=$(kubectl get configmap "${RELEASE}-config" -n "${NAMESPACE}" -o jsonpath='{.data.omneval\.yaml}' 2>/dev/null | grep -A2 backup | grep enabled | awk '{print $2}' || true)
if [[ "${BACKUP_ENABLED}" == "true" ]]; then
  pass "backup.enabled = true"
else
  fail "backup.enabled = ${BACKUP_ENABLED:-<unset>} (expected: true)"
fi

# Check 3: Quack Server PVC exists
echo ""
echo "3. Checking Quack Server PVC..."
PVC_EXISTS=$(kubectl get pvc -l app.kubernetes.io/component=quack-server -n "${NAMESPACE}" -o name 2>/dev/null || true)
if [[ -n "${PVC_EXISTS}" ]]; then
  pass "PVC exists: ${PVC_EXISTS}"
else
  fail "No PVC found for quack-server"
fi

# Check 4: Quack Server pod is running
echo ""
echo "4. Checking Quack Server health..."
QUACK_READY=$(kubectl get statefulset "${RELEASE}-quack-server" -n "${NAMESPACE}" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)
if [[ "${QUACK_READY}" == "1" ]]; then
  pass "Quack Server is ready (1/1 replicas)"
else
  fail "Quack Server ready replicas: ${QUACK_READY:-0}/1"
fi

# Check 5: Catalog file path exists in Quack Server pod
echo ""
echo "5. Checking catalog file path..."
CATALOG_PATH=$(kubectl get configmap "${RELEASE}-config" -n "${NAMESPACE}" -o jsonpath='{.data.omneval\.yaml}' 2>/dev/null | grep catalog_dsn | awk '{print $2}' | tr -d '"' || true)
if [[ -z "${CATALOG_PATH}" ]]; then
  CATALOG_PATH="lake/catalog.duckdb"
fi

# Verify catalog file exists inside the Quack Server pod
# If CATALOG_PATH is absolute (starts with /), use it directly; otherwise prepend /data/
if [[ "${CATALOG_PATH}" == /* ]]; then
  CATALOG_CHECK_PATH="${CATALOG_PATH}"
else
  CATALOG_CHECK_PATH="/data/${CATALOG_PATH}"
fi
CATALOG_FILE_EXISTS=$(kubectl exec "${RELEASE}-quack-server-0" -n "${NAMESPACE}" -- ls "${CATALOG_CHECK_PATH}" 2>/dev/null || true)
if [[ -n "${CATALOG_FILE_EXISTS}" ]]; then
  pass "Catalog file exists at ${CATALOG_CHECK_PATH}"
else
  fail "Catalog file not found at ${CATALOG_CHECK_PATH}"
fi

# Check 6: Old Postgres Catalog config removed
echo ""
echo "6. Checking old Postgres Catalog config removed..."
POSTGRES_CONFIG=$(kubectl get configmap "${RELEASE}-config" -n "${NAMESPACE}" -o jsonpath='{.data.omneval\.yaml}' 2>/dev/null | grep -E 'catalog.*postgres|catalog.*Postgres' | head -5 || true)
if [[ -z "${POSTGRES_CONFIG}" ]]; then
  pass "No old Postgres Catalog config found in configmap"
else
  fail "Old Postgres Catalog config still present:"
  echo "    ${POSTGRES_CONFIG}"
fi

# Check 7: Writer pods are running (after resume)
echo ""
echo "7. Checking Writer pods..."
WRITER_PODS=$(kubectl get pods -n "${NAMESPACE}" -l app.kubernetes.io/component=writer -o name 2>/dev/null || true)
WRITER_COUNT=$(kubectl get pods -n "${NAMESPACE}" -l app.kubernetes.io/component=writer -o name 2>/dev/null | wc -l || echo "0")
if [[ "${WRITER_COUNT}" -gt 0 ]]; then
  pass "Writer pods running: ${WRITER_COUNT}"
else
  fail "No Writer pods running (cutover may not be complete)"
fi

# Check 8: All services healthy
echo ""
echo "8. Checking all Omneval services..."
ALL_PODS=$(kubectl get pods -n "${NAMESPACE}" -o name 2>/dev/null | grep -E '(omneval-)?(ingest|writer|query|eval|quack)' || true)
if [[ -n "${ALL_PODS}" ]]; then
  pass "Omneval pods found"
  MANUAL_HINT="Check all pods are Running/Ready: $(echo "${ALL_PODS}" | tr '\n' ' ')"
else
  fail "No Omneval service pods found"
fi

# Summary
echo ""
echo "================================================================"
echo "Automated checks: ${PASSED} passed, ${FAILED} failed"
echo "Manual checks remaining: ${MANUAL}"
if [[ -n "${MANUAL_HINT}" ]]; then
  echo "  Hint: ${MANUAL_HINT}"
fi
echo "================================================================"

if [[ ${FAILED} -gt 0 ]]; then
  echo ""
  echo "❌ Automated validation FAILED. Review the failures above."
  echo "   See docs/runbooks/catalog-cutover.md for the full runbook."
  exit 1
fi

echo ""
echo "✅ All automated checks passed."
echo ""
echo "Now execute the remaining MANUAL checks from the runbook:"
echo ""
echo "  Check 1 — Writer commit:"
echo "    1. Ingest a test span via the Ingest API"
echo "    2. Wait ~5s for the Writer's commit cadence"
echo "    3. Query the Lake to verify the span appears"
echo ""
echo "  Check 2 — Eval score write-back:"
echo "    1. Find a span ID via the Query API"
echo "    2. POST a score to the Writer's internal score endpoint"
echo "    3. Verify the score appears in the trace detail"
echo ""
echo "  Check 3 — Query read all:"
echo "    1. Read the total span count via the Query API"
echo "    2. Compare against the pre-cutover count (recorded in Phase 2 of the runbook)"
echo "    3. Read a specific trace waterfall (end-to-end trace read)"
echo ""
echo "  Final checks:"
echo "    1. Monitor Writer logs for 30 min (no tx conflict/attach race errors)"
echo "    2. Verify Eval scores flowing in"
echo "    3. Confirm backup scheduler is running (CHECKPOINT/backup uploaded in logs)"
echo ""
echo "  See docs/runbooks/catalog-cutover.md for all details."
exit 0
