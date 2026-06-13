package domain

import "time"

// RetentionAction defines what the retention worker does with aged objects.
type RetentionAction string

const (
	// ActionDelete permanently removes aged objects from S3.
	ActionDelete RetentionAction = "delete"
	// ActionMove copies aged objects to a destination bucket before deleting
	// the source objects.
	ActionMove RetentionAction = "move"
)

// RetentionPolicy is the retention rule applied by the writer retention worker.
type RetentionPolicy struct {
	// Action is the retention action to perform: delete or move.
	Action RetentionAction `json:"action"`
	// MaxAgeDays is the number of days after which objects are eligible for
	// retention. Zero means no age limit (disabled).
	MaxAgeDays int `json:"max_age_days"`
	// Destination is the target bucket for move actions.
	Destination ArchiveDestination `json:"destination,omitempty"`
	// Enabled controls whether the retention worker runs at all.
	Enabled bool `json:"enabled"`
	// IntervalMinutes is how often the worker scans for eligible objects.
	IntervalMinutes int `json:"interval_minutes"`
}

// IsDelete returns true if the action is ActionDelete.
func (p RetentionPolicy) IsDelete() bool {
	return p.Action == ActionDelete
}

// IsMove returns true if the action is ActionMove.
func (p RetentionPolicy) IsMove() bool {
	return p.Action == ActionMove
}

// ArchiveDestination describes where aged objects should be moved.
type ArchiveDestination struct {
	// Provider identifies the storage provider: "s3", "azure", "gcs".
	Provider string `json:"provider"`
	// Bucket is the destination bucket/container name.
	Bucket string `json:"bucket"`
	// Prefix is an optional key prefix within the destination bucket.
	Prefix string `json:"prefix"`
	// StorageClass is the target storage class (e.g. "GLACIER", "STANDARD_IA").
	StorageClass string `json:"storage_class"`
	// Account is the Azure storage account name (only for Azure provider).
	Account string `json:"account,omitempty"`
}

// S3URI returns the S3 URI for the destination, e.g. "s3://cold-archive/omneval/".
func (d ArchiveDestination) S3URI() string {
	return "s3://" + d.Bucket + "/" + d.Prefix
}

// AzureURI returns the Azure Blob URI for the destination.
func (d ArchiveDestination) AzureURI() string {
	if d.Account == "" {
		return ""
	}
	return "https://" + d.Account + ".blob.core.windows.net/" + d.Bucket
}

// RotationResult records the outcome of a single retention run.
type RotationResult struct {
	ObjectsScanned int           `json:"objects_scanned"`
	ObjectsActedOn int           `json:"objects_acted_on"`
	BytesActedOn   int64         `json:"bytes_acted_on"`
	Errors         []error       `json:"errors,omitempty"`
	Duration       time.Duration `json:"duration_seconds"`
}
