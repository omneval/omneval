# Helm Examples

Production-ready Helm values overrides and scripts for common deployment scenarios.

## Catalog Cutover (DuckDB File Catalog)

### Files

| File | Purpose |
|---|---|
| [production-catalogduckdb-values.yaml](production-catalogduckdb-values.yaml) | Production Helm values override for migrating from Postgres Catalog to DuckDB file Catalog |
| [post-cutover-validation.sh](post-cutover-validation.sh) | Operator checklist script that validates the cutover was successful |
| [../catalog-cutover.md](../../../docs/runbooks/catalog-cutover.md) | The full runbook (phased migration procedure) |

### When to use

Use `production-catalogduckdb-values.yaml` when performing the planned cutover from `CatalogDriverPostgres` to `CatalogDriverLocal` (DuckDB file catalog). This is documented in the [Catalog cutover runbook](../../../docs/runbooks/catalog-cutover.md).

### Usage

```bash
# Dry-run the upgrade
helm upgrade omneval ./deploy/helm --values production-catalogduckdb-values.yaml --dry-run --debug

# Execute the upgrade (after following the runbook's pre-flight checks)
helm upgrade omneval ./deploy/helm --values production-catalogduckdb-values.yaml

# Validate
./post-cutover-validation.sh <release> <namespace>
```

> **⚠️ Warning:** This is a human-in-the-loop (HITL) operation. See the [runbook](../../../docs/runbooks/catalog-cutover.md) for the full phased procedure with downtime window management.