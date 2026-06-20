# Helm Examples

Production-ready Helm values overrides and scripts for common deployment scenarios.

## Catalog recovery from Parquet

### Files

| File | Purpose |
|---|---|
| [production-catalogduckdb-values.yaml](production-catalogduckdb-values.yaml) | Production Helm values override for `CatalogDriverLocal` (DuckDB file catalog) |
| [post-cutover-validation.sh](post-cutover-validation.sh) | Operator checklist script that validates catalog recovery |
| [../../../docs/runbooks/catalog-rebuild-from-parquet.md](../../../docs/runbooks/catalog-rebuild-from-parquet.md) | The full runbook (last-resort disaster recovery procedure) |

### When to use

Use `production-catalogduckdb-values.yaml` as the production Helm values override for a `CatalogDriverLocal` deployment. If the Catalog file is lost and no PVC backup exists, follow the [Rebuild Catalog from Parquet runbook](../../../docs/runbooks/catalog-rebuild-from-parquet.md).

### Usage

```bash
# Dry-run the upgrade
helm upgrade omneval ./deploy/helm --values production-catalogduckdb-values.yaml --dry-run --debug

# Execute the upgrade
helm upgrade omneval ./deploy/helm --values production-catalogduckdb-values.yaml

# Validate (after catalog recovery)
./post-cutover-validation.sh <release> <namespace>
```

> **⚠️ Warning:** If the Catalog is lost, this is a human-in-the-loop (HITL) operation. See the [runbook](../../../docs/runbooks/catalog-rebuild-from-parquet.md) for the full procedure with downtime window management.