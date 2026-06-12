# Writer Service runs as a StatefulSet with a PVC-mounted DuckDB file

Status: superseded by ADR-0004 (DuckLake storage core)

DuckDB enforces a single-writer constraint — only one process may open the file read-write at a time. We considered bootstrapping from an S3 snapshot on each pod start (stateless deployment) and using a distributed lock to gate write access, but both introduce a window where concurrent writers could corrupt the file or where a lock failure leaves the file in an unrecoverable state. Instead, the Writer Service runs as a Kubernetes StatefulSet with a PersistentVolumeClaim. The DuckDB file lives on the PVC; the pod that mounts it is the sole writer by OS file-lock. Pod replacement (crash, rolling update) re-mounts the same PVC and reopens the same file — no recovery protocol required.

## Considered Options

- **S3-bootstrap + stateless deployment**: each Writer pod downloads the latest snapshot from S3 on startup and races to become writer. Rejected because the bootstrap adds startup latency and does not prevent two pods from writing simultaneously during an overlap window.
- **Distributed lock (e.g. Redis SETNX)**: a leader-election lock gates write access. Rejected because lock expiry and crash recovery introduce edge cases where the file is left locked while a dead pod still holds it, requiring manual intervention.
- **StatefulSet + PVC** (chosen): Kubernetes guarantees at most one pod mounts the PVC read-write. The OS file lock is the backstop. Recovery is automatic via pod replacement.
