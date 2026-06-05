package domain

import (
	"testing"
)

func TestRetentionPolicy_IsDelete(t *testing.T) {
	policy := RetentionPolicy{Action: ActionDelete}
	if !policy.IsDelete() {
		t.Error("expected IsDelete() == true for ActionDelete")
	}
	if policy.IsMove() {
		t.Error("expected IsMove() == false for ActionDelete")
	}
}

func TestRetentionPolicy_IsMove(t *testing.T) {
	policy := RetentionPolicy{Action: ActionMove}
	if !policy.IsMove() {
		t.Error("expected IsMove() == true for ActionMove")
	}
	if policy.IsDelete() {
		t.Error("expected IsDelete() == false for ActionMove")
	}
}

func TestArchiveDestination_S3URI(t *testing.T) {
	dst := ArchiveDestination{
		Provider: "s3",
		Bucket:   "cold-archive",
		Prefix:   "omneval/",
	}
	want := "s3://cold-archive/omneval/"
	if got := dst.S3URI(); got != want {
		t.Errorf("S3URI() = %q, want %q", got, want)
	}
}

func TestArchiveDestination_AzureURI(t *testing.T) {
	dst := ArchiveDestination{
		Provider: "azure",
		Bucket:   "mycontainer",
		Account:  "mystorage",
	}
	want := "https://mystorage.blob.core.windows.net/mycontainer"
	if got := dst.AzureURI(); got != want {
		t.Errorf("AzureURI() = %q, want %q", got, want)
	}
}

func TestArchiveDestination_AzureURI_EmptyAccount(t *testing.T) {
	dst := ArchiveDestination{
		Provider: "azure",
		Bucket:   "mycontainer",
	}
	if got := dst.AzureURI(); got != "" {
		t.Errorf("AzureURI() with empty account = %q, want empty", got)
	}
}

func TestRotationResult_Fields(t *testing.T) {
	result := RotationResult{
		ObjectsScanned: 10,
		ObjectsActedOn: 5,
		BytesActedOn:   1024,
		Errors:         []error{nil},
	}
	if result.ObjectsScanned != 10 {
		t.Errorf("ObjectsScanned = %d, want 10", result.ObjectsScanned)
	}
	if result.ObjectsActedOn != 5 {
		t.Errorf("ObjectsActedOn = %d, want 5", result.ObjectsActedOn)
	}
	if result.BytesActedOn != 1024 {
		t.Errorf("BytesActedOn = %d, want 1024", result.BytesActedOn)
	}
	if len(result.Errors) != 1 {
		t.Errorf("Errors length = %d, want 1", len(result.Errors))
	}
}
