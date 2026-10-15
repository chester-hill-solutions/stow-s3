package embedded

import (
	"context"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// A key the store does not have is a confirmed delete, matching S3's idempotent
// delete and the contract the built-in backends honour. The adapter used to fail
// the whole batch on it.
type missingKeyStore struct{ Store }

func (missingKeyStore) DeleteObject(context.Context, string, string) error {
	return storage.ErrObjectNotFound
}

func TestDeleteObjectsConfirmsAMissingKey(t *testing.T) {
	adapter := &storeAdapter{store: missingKeyStore{}}
	confirmed, err := adapter.DeleteObjects(context.Background(), "bucket", []string{"absent"})
	if err != nil {
		t.Fatalf("DeleteObjects: %v", err)
	}
	if len(confirmed) != 1 || confirmed[0] != "absent" {
		t.Fatalf("confirmed = %v, want [absent]", confirmed)
	}
}
