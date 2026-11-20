package workspace

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestWorkspaceFileSaveHonoursAConfiguredBoundAboveSixtyFourKiB(t *testing.T) {
	store := newFileSaveJournalStore(t)
	payload := bytes.Repeat([]byte("a"), 100<<10)
	receipt, err := store.SaveWorkspaceFile(context.Background(), "bucket", storage.WorkspaceFileSaveRequest{
		RequestKey: "large-save",
		Write: storage.WorkspaceFileWriteRequest{
			Path: "large.bin", Data: payload, Replace: true, MaxFileBytes: 100 << 10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome != string(storage.WorkspaceFileCommitted) || receipt.Meta == nil || receipt.Meta.Size != int64(len(payload)) {
		t.Fatalf("receipt = %+v", receipt)
	}
}

func TestWorkspaceFileSaveRefusesAboveTheSupportedCeiling(t *testing.T) {
	store := newFileSaveJournalStore(t)
	payload := bytes.Repeat([]byte("a"), 32)
	_, err := store.SaveWorkspaceFile(context.Background(), "bucket", storage.WorkspaceFileSaveRequest{
		RequestKey: "oversized-bound",
		Write: storage.WorkspaceFileWriteRequest{
			Path: "small.bin", Data: payload, Replace: true, MaxFileBytes: storage.MaxWorkspaceFileBytes + 1,
		},
	})
	if !errors.Is(err, storage.ErrInvalidSaveRequest) {
		t.Fatalf("oversized bound = %v", err)
	}
}
