# Restoring from a VolumeSnapshot

When the Writer PVC is lost (disk failure, accidental deletion, node failure),
you can recover the DuckDB database from a Kubernetes `VolumeSnapshot`.

## Prerequisites

- A `VolumeSnapshot` resource exists in your cluster (created by the
  `writer-volume-snapshot` CronJob, or manually)
- Your cloud CSI driver supports snapshot-based PVC restoration (EBS, GCE PD,
  Azure Disk, etc.)

## Listing available snapshots

```bash
# List all VolumeSnapshots in the omneval namespace
kubectl get volumesnapshots -n omneval

# List snapshots tagged as Omneval backups
kubectl get volumesnapshots -n omneval -l app.kubernetes.io/component=writer

# Get details of a specific snapshot (shows source PVC, ready status, size)
kubectl describe volumesnapshot omneval-writer-20260507-020000 -n omneval
```

Example output:

```
NAME                       READYTOUSE   SOURCEPVC        SOURCESVCPNAME   SIZE       RESTORESIZE   CLASS                AGE
omneval-writer-20260507    10Gi         data-writer-0    ebs-csi-gp3      10Gi       10Gi          ebs-csi-gp3          2d
```

The `READYTOUSE` column shows whether the snapshot is ready for restoration.
A numeric value means ready; `false` means the snapshot is still being
finalized by the CSI driver.

## Creating a PVC from a snapshot

To restore from a snapshot, create a new PVC that references the snapshot:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data-writer-0-restore
  namespace: omneval
spec:
  accessModes:
    - ReadWriteOnce
  # Reference the VolumeSnapshot by name
  dataSource:
    name: omneval-writer-20260507-020000
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
  resources:
    requests:
      storage: 10Gi
```

Apply the PVC:

```bash
kubectl apply -f restore-pvc.yaml
```

Wait for the PVC to be bound (the CSI driver clones the snapshot data):

```bash
kubectl get pvc data-writer-0-restore -n omneval
# NAME                    STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
# data-writer-0-restore   Bound    pvc-abc123def456                           10Gi       RWO            gp3            30s
```

## Restoring the Writer StatefulSet

### Option A: Patch the StatefulSet (recommended for quick recovery)

Replace the original PVC reference in the StatefulSet's volumeClaimTemplates
with the restored PVC. This avoids recreating the entire StatefulSet.

```bash
# Edit the writer StatefulSet to use the restored PVC
kubectl patch statefulset writer -n omneval --type merge -p '
{
  "spec": {
    "template": {
      "spec": {
        "volumes": null,
        "containers": [{
          "name": "writer",
          "volumeMounts": [{
            "name": "data",
            "mountPath": "/data"
          }]
        }]
      }
    },
    "volumeClaimTemplates": [
      {
        "metadata": {
          "name": "data"
        },
        "spec": {
          "accessModes": ["ReadWriteOnce"],
          "volumeName": "",
          "dataSource": {
            "name": "omneval-writer-20260507-020000",
            "kind": "VolumeSnapshot",
            "apiGroup": "snapshot.storage.k8s.io"
          },
          "resources": {
            "requests": {
              "storage": "10Gi"
            }
          }
        }
      }
    ]
  }
}'
```

Then delete the writer pod to force a recreation with the new PVC:

```bash
kubectl delete pod writer-0 -n omneval
```

The StatefulSet controller will create a new PVC from the snapshot, and the
writer pod will mount it.

### Option B: Recreate the StatefulSet (cleanest, but requires downtime)

1. Delete the existing StatefulSet (preserving the pods):

```bash
kubectl delete statefulset writer -n omneval --cascade=orphan
```

2. Apply your modified manifest that references the restored PVC:

```bash
# Edit the StatefulSet spec to use dataSource pointing to the snapshot
# Then apply
kubectl apply -f writer-statefulset-restored.yaml
```

3. Create the writer pod from the template:

```bash
kubectl create -f <(kubectl get statefulset writer -n omneval -o yaml | sed 's/name: writer/name: writer-new/')
```

## Verifying the restore

After the writer pod is running, verify the DuckDB file is intact:

```bash
# Check the writer pod is ready
kubectl get pods -n omneval -l app.kubernetes.io/component=writer

# Exec into the pod and check the DuckDB file
kubectl exec -it writer-0 -n omneval -- duckdb /data/omneval.duckdb -c "SELECT count(*) FROM spans;"

# Check recent spans are accessible via the Query API
curl -s http://omneval-query:8002/api/v1/spans?limit=1 | jq
```

## Cleaning up old snapshots

Old snapshots consume storage and should be cleaned up periodically:

```bash
# Delete snapshots older than 7 days
kubectl delete volumesnapshots -n omneval -l app.kubernetes.io/component=writer \
  --field-selector metadata.creationTimestamp<$(date -d '7 days ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -v-7d +%Y-%m-%dT%H:%M:%SZ)

# List snapshots for manual review
kubectl get volumesnapshots -n omneval -o custom-columns='NAME:metadata.name,CREATED:metadata.creationTimestamp,SIZE:status.readyToUse' \
  --sort-by=metadata.creationTimestamp
```

Or set up a separate CronJob to automate cleanup:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: omneval-cleanup-old-snapshots
  namespace: omneval
spec:
  schedule: "0 3 * * 0"  # Every Sunday at 03:00 UTC
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: omneval-volume-snapshot
          containers:
            - name: cleanup
              image: bitnami/kubectl:latest
              command:
                - /bin/sh
                - -c
                - |
                  # Delete snapshots older than 7 days
                  kubectl delete volumesnapshots -n omneval \
                    -l app.kubernetes.io/component=writer \
                    --field-selector metadata.creationTimestamp<$(date -d '7 days ago' +%Y-%m-%dT%H:%M:%SZ)
          restartPolicy: OnFailure
```

## Troubleshooting

### Snapshot is not ready

If `READYTOUSE` is `false`, the CSI driver is still creating the snapshot.
Wait and retry:

```bash
kubectl wait volumesnapshot omneval-writer-20260507-020000 -n omneval \
  --for=jsonpath='{.status.readyToUse}'=true --timeout=300s
```

### PVC restore fails

Check the PVC events for errors:

```bash
kubectl describe pvc data-writer-0-restore -n omneval
```

Common issues:
- **Snapshot class mismatch**: The snapshot was created with a different
  `VolumeSnapshotClass` than your cluster supports
- **Insufficient storage**: The restored PVC is larger than available storage
- **Zone mismatch**: The snapshot was created in one availability zone but
  the new PVC is scheduled in another (AWS EBS limitation)

### Writer pod stuck in Pending

Check if the PVC is bound:

```bash
kubectl get pvc -n omneval
```

If the PVC is Pending, check the events:

```bash
kubectl describe pvc data-writer-0-restore -n omneval
```

## Backfill the legacy stores into the Lake (ADR-0004)

The DuckLake migration replaces the hot/snapshot/cold tiers with the Lake. The data-migration half of the cutover is the `backfill` subcommand on the writer binary:

```bash
# inside the writer pod / with the writer's config available
writer backfill
# or with explicit sources:
writer backfill -hot-db /data/omneval.db -archive s3://omneval/archive
```

It reads both legacy tiers — the hot DuckDB file (`writer.duckdb_path`) and the Hive-partitioned cold Parquet archive (`s3://<storage.bucket>/archive`) — and inserts their spans and scores into the Lake, preserving the `(project_id, span-date)` partitioning. Scores partition next to their span (the span's `start_time` date), falling back to the score's `created_at` when the span is unknown.

Properties:

- **Idempotent.** Each partition is delete-and-rewritten in one Lake transaction; re-running the command produces identical Lake row counts. Pre-existing duplicate rows in a Lake partition (residual ledger-crash duplicates) are healed by the rewrite.
- **Hot-window overlap is deduplicated.** A span present in both the hot store and the archive lands exactly once, keyed on `(trace_id, span_id)`.
- **Verifiable.** On completion the command prints a per-`(project, date)` table of source vs Lake row counts for spans and scores, and exits nonzero if any partition mismatches. Run it (and verify) before flipping `query.lake.enabled` in production, and keep the legacy stores until the report is clean.
- **Read-only on the sources.** The hot DuckDB file is attached `READ_ONLY`; the archive is only read.
