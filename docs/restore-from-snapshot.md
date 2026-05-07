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
# List all VolumeSnapshots in the lantern namespace
kubectl get volumesnapshots -n lantern

# List snapshots tagged as Lantern backups
kubectl get volumesnapshots -n lantern -l app.kubernetes.io/component=writer

# Get details of a specific snapshot (shows source PVC, ready status, size)
kubectl describe volumesnapshot lantern-writer-20260507-020000 -n lantern
```

Example output:

```
NAME                       READYTOUSE   SOURCEPVC        SOURCESVCPNAME   SIZE       RESTORESIZE   CLASS                AGE
lantern-writer-20260507    10Gi         data-writer-0    ebs-csi-gp3      10Gi       10Gi          ebs-csi-gp3          2d
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
  namespace: lantern
spec:
  accessModes:
    - ReadWriteOnce
  # Reference the VolumeSnapshot by name
  dataSource:
    name: lantern-writer-20260507-020000
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
kubectl get pvc data-writer-0-restore -n lantern
# NAME                    STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
# data-writer-0-restore   Bound    pvc-abc123def456                           10Gi       RWO            gp3            30s
```

## Restoring the Writer StatefulSet

### Option A: Patch the StatefulSet (recommended for quick recovery)

Replace the original PVC reference in the StatefulSet's volumeClaimTemplates
with the restored PVC. This avoids recreating the entire StatefulSet.

```bash
# Edit the writer StatefulSet to use the restored PVC
kubectl patch statefulset writer -n lantern --type merge -p '
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
            "name": "lantern-writer-20260507-020000",
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
kubectl delete pod writer-0 -n lantern
```

The StatefulSet controller will create a new PVC from the snapshot, and the
writer pod will mount it.

### Option B: Recreate the StatefulSet (cleanest, but requires downtime)

1. Delete the existing StatefulSet (preserving the pods):

```bash
kubectl delete statefulset writer -n lantern --cascade=orphan
```

2. Apply your modified manifest that references the restored PVC:

```bash
# Edit the StatefulSet spec to use dataSource pointing to the snapshot
# Then apply
kubectl apply -f writer-statefulset-restored.yaml
```

3. Create the writer pod from the template:

```bash
kubectl create -f <(kubectl get statefulset writer -n lantern -o yaml | sed 's/name: writer/name: writer-new/')
```

## Verifying the restore

After the writer pod is running, verify the DuckDB file is intact:

```bash
# Check the writer pod is ready
kubectl get pods -n lantern -l app.kubernetes.io/component=writer

# Exec into the pod and check the DuckDB file
kubectl exec -it writer-0 -n lantern -- duckdb /data/lantern.duckdb -c "SELECT count(*) FROM spans;"

# Check recent spans are accessible via the Query API
curl -s http://lantern-query:8002/api/v1/spans?limit=1 | jq
```

## Cleaning up old snapshots

Old snapshots consume storage and should be cleaned up periodically:

```bash
# Delete snapshots older than 7 days
kubectl delete volumesnapshots -n lantern -l app.kubernetes.io/component=writer \
  --field-selector metadata.creationTimestamp<$(date -d '7 days ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -v-7d +%Y-%m-%dT%H:%M:%SZ)

# List snapshots for manual review
kubectl get volumesnapshots -n lantern -o custom-columns='NAME:metadata.name,CREATED:metadata.creationTimestamp,SIZE:status.readyToUse' \
  --sort-by=metadata.creationTimestamp
```

Or set up a separate CronJob to automate cleanup:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: lantern-cleanup-old-snapshots
  namespace: lantern
spec:
  schedule: "0 3 * * 0"  # Every Sunday at 03:00 UTC
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: lantern-volume-snapshot
          containers:
            - name: cleanup
              image: bitnami/kubectl:latest
              command:
                - /bin/sh
                - -c
                - |
                  # Delete snapshots older than 7 days
                  kubectl delete volumesnapshots -n lantern \
                    -l app.kubernetes.io/component=writer \
                    --field-selector metadata.creationTimestamp<$(date -d '7 days ago' +%Y-%m-%dT%H:%M:%SZ)
          restartPolicy: OnFailure
```

## Troubleshooting

### Snapshot is not ready

If `READYTOUSE` is `false`, the CSI driver is still creating the snapshot.
Wait and retry:

```bash
kubectl wait volumesnapshot lantern-writer-20260507-020000 -n lantern \
  --for=jsonpath='{.status.readyToUse}'=true --timeout=300s
```

### PVC restore fails

Check the PVC events for errors:

```bash
kubectl describe pvc data-writer-0-restore -n lantern
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
kubectl get pvc -n lantern
```

If the PVC is Pending, check the events:

```bash
kubectl describe pvc data-writer-0-restore -n lantern
```
