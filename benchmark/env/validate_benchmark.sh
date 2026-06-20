#!/usr/bin/env bash
# ============================================================================
# validate_benchmark.sh — Verify the benchmark environment is healthy
#
# Checks that all pods are running and their health/readiness endpoints
# return 200 OK.
#
# Prerequisites:
#   - kubectl configured with access to the cluster
#   - A port-forward for the Ingest API and Query API services
#
# Usage:
#   ./validate_benchmark.sh
#
# Environment variables:
#   OMNEVAL_NAMESPACE — Kubernetes namespace (default: omneval-benchmark)
#   OMNEVAL_RELEASE   — Helm release name  (default: omneval-benchmark)
#
# Port-forward targets:
#   Ingest API:  localhost:8000
#   Query API:   localhost:8002
#
# See README.md for how to set up port-f forwards.
# ============================================================================

set -euo pipefail

NAMESPACE="${OMNEVAL_NAMESPACE:-omneval-benchmark}"
RELEASE="${OMNEVAL_RELEASE:-omneval-benchmark}"

PASS=0
FAIL=0
WARN=0

pass() { echo "  ✅ $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ $1"; FAIL=$((FAIL + 1)); }
warn() { echo "  ⚠️  $1"; WARN=$((WARN + 1)); }

echo "=== Omneval Benchmark Environment — Validation ==="
echo ""

# ── 1. Check pods exist and are Ready ──────────────────────────────────────
echo "--- Pod Status ---"
PODS=$(kubectl get pods \
  --namespace "$NAMESPACE" \
  --selector "app.kubernetes.io/instance=$RELEASE" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.phase}{"\t"}{range .status.conditions[*]}{.type}={.status}{" "}{end}{"\n"}{end}' 2>/dev/null || true)

if [[ -z "$PODS" ]]; then
  fail "No pods found with selector app.kubernetes.io/instance=$RELEASE"
else
  echo "$PODS" | while IFS=$'\t' read -r name phase conditions; do
    if [[ "$phase" == "Running" ]] && echo "$conditions" | grep -q "Ready=True"; then
      echo "  ✅ $name — Running, Ready"
    elif [[ "$phase" == "Running" ]]; then
      echo "  ⚠️  $name — Running but not yet Ready: $conditions"
    else
      echo "  ❌ $name — Phase: $phase, Conditions: $conditions"
    fi
  done
fi

# ── 2. Check pod counts ───────────────────────────────────────────────────
echo ""
echo "--- Pod Counts ---"
EXPECTED_TOTAL=6  # ingest, writer, query, quack, postgres, redis
ACTUAL_TOTAL=$(kubectl get pods \
  --namespace "$NAMESPACE" \
  --selector "app.kubernetes.io/instance=$RELEASE" \
  -o jsonpath='{.items[*].metadata.name}' 2>/dev/null | wc -w)

if [[ "$ACTUAL_TOTAL" -ge 6 ]]; then
  pass "At least $EXPECTED_TOTAL pods found ($ACTUAL_TOTAL total)"
else
  fail "Expected at least $EXPECTED_TOTAL pods, found $ACTUAL_TOTAL"
fi

# ── 3. Check Ingest API health ─────────────────────────────────────────────
echo ""
echo "--- Ingest API Health (http://localhost:8000/healthz) ---"
if curl -sf http://localhost:8000/healthz >/dev/null 2>&1; then
  pass "Ingest API /healthz returns 200"
else
  warn "Ingest API /healthz not reachable — ensure port-forward is active:"
  warn "  kubectl port-forward --namespace $NAMESPACE svc/${RELEASE}-ingest 8000:8000"
fi

echo ""
echo "--- Ingest API Readiness (http://localhost:8000/readyz) ---"
if curl -sf http://localhost:8000/readyz >/dev/null 2>&1; then
  pass "Ingest API /readyz returns 200"
else
  warn "Ingest API /readyz not reachable"
fi

# ── 4. Check Query API health ──────────────────────────────────────────────
echo ""
echo "--- Query API Health (http://localhost:8002/healthz) ---"
if curl -sf http://localhost:8002/healthz >/dev/null 2>&1; then
  pass "Query API /healthz returns 200"
else
  warn "Query API /healthz not reachable — ensure port-forward is active:"
  warn "  kubectl port-forward --namespace $NAMESPACE svc/${RELEASE}-query 8002:8002"
fi

echo ""
echo "--- Query API Readiness (http://localhost:8002/readyz) ---"
if curl -sf http://localhost:8002/readyz >/dev/null 2>&1; then
  pass "Query API /readyz returns 200"
else
  warn "Query API /readyz not reachable"
fi

# ── 5. Check Quack Server internal health ───────────────────────────────────
echo ""
echo "--- Quack Server Health (internal port-forward:9090) ---"
QUACK_POD=$(kubectl get pods \
  --namespace "$NAMESPACE" \
  --selector "app.kubernetes.io/component=quack-server" \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [[ -n "$QUACK_POD" ]]; then
  if QUIET=$(kubectl port-forward --namespace "$NAMESPACE" pod/"$QUACK_POD" 9090:9090 2>/dev/null &); then
    sleep 2
    if curl -sf http://localhost:9090/healthz >/dev/null 2>&1; then
      pass "Quack Server /healthz returns 200"
    else
      warn "Quack Server /healthz not reachable"
    fi
    kill $QUIET 2>/dev/null || true
    wait $QUIET 2>/dev/null || true
  else
    warn "Could not port-forward to Quack Server pod: $QUACK_POD"
  fi
else
  warn "Quack Server pod not found"
fi

# ── 6. Check services exist ────────────────────────────────────────────────
echo ""
echo "--- Service List ---"
SERVICES=$(kubectl get svc --namespace "$NAMESPACE" \
  --selector "app.kubernetes.io/instance=$RELEASE" \
  -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true)

if [[ -n "$SERVICES" ]]; then
  for svc in $SERVICES; do
    echo "  ✅ $svc"
  done
else
  warn "No services found with selector app.kubernetes.io/instance=$RELEASE"
fi

# ── 7. Check S3 connectivity (Quack Server can write to the bucket) ────────
echo ""
echo "--- S3 Bucket Check ---"
# Check if the S3 bucket is configured in the configmap
S3_BUCKET=$(kubectl get configmap "${RELEASE}-config" \
  --namespace "$NAMESPACE" \
  -o jsonpath='{.data.omneval\.yaml}' 2>/dev/null | grep -oP '(?<=bucket:\s+)\S+' | head -1 || echo "")

if [[ -n "$S3_BUCKET" ]]; then
  pass "S3 bucket configured: $S3_BUCKET"
else
  warn "Could not determine S3 bucket from configmap"
fi

# ── Summary ─────────────────────────────────────────────────────────────────
echo ""
echo "=== Validation Summary ==="
echo "  ✅ Passed: $PASS"
echo "  ❌ Failed: $FAIL"
echo "  ⚠️  Warned:  $WARN"
echo ""

if [[ $FAIL -gt 0 ]]; then
  echo "STATUS: FAILED — $FAIL check(s) did not pass"
  exit 1
elif [[ $WARN -gt 0 ]]; then
  echo "STATUS: WARNINGS — $WARN check(s) had issues"
  echo "  Check the warnings above; many are expected if port-fors"
  exit 0
else
  echo "STATUS: ALL CHECKS PASSED"
  exit 0
fi