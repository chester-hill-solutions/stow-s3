package runtime_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestReadFailureRetainsRetryableCleanup(t *testing.T) {
	t.Run("premature", func(t *testing.T) { testReadFailureCleanup(t, 7, "bytes") })
	t.Run("excess", func(t *testing.T) { testReadFailureCleanup(t, 3, "byt") })
}

func testReadFailureCleanup(t *testing.T, size int64, want string) {
	t.Helper()
	ctx := context.Background()
	store := &observedStreamStore{Store: storage.NewMemoryStore(), closeFailures: 1, declaredSize: size}
	if err := store.CreateBucket(ctx, "assets"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "assets", "document", strings.NewReader("bytes"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	instance, err := runtime.OpenWithStore(runtime.Options{}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	reader, _, err := instance.OpenObject(ctx, "assets", "document")
	if err != nil {
		t.Fatal(err)
	}
	assertFailedReadClosed(t, reader, want)
	assertReadCleanupRetry(t, reader, store)
}

func assertFailedReadClosed(t *testing.T, reader io.ReadCloser, want string) {
	t.Helper()
	data, readErr := io.ReadAll(reader)
	if string(data) != want || readErr == nil || readErr == io.EOF {
		t.Fatalf("read=%q err=%v", data, readErr)
	}
	if want == "bytes" && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("premature EOF lost: %v", readErr)
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("failed stream remains readable: %v", err)
	}
}

func assertReadCleanupRetry(t *testing.T, reader io.ReadCloser, store *observedStreamStore) {
	t.Helper()
	if store.closes != 1 {
		t.Fatalf("automatic cleanup attempts=%d", store.closes)
	}
	if err := reader.Close(); err != nil || store.closes != 2 {
		t.Fatalf("cleanup retry count=%d err=%v", store.closes, err)
	}
	if err := reader.Close(); err != nil || store.closes != 2 {
		t.Fatalf("idempotent cleanup count=%d err=%v", store.closes, err)
	}
}
