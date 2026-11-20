package directory_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence/adaptertest"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence/directory"
)

func TestDirectoryAdapter(t *testing.T) {
	adaptertest.Run(t, func(t *testing.T) persistence.Adapter {
		adapter, err := directory.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = adapter.Close() })
		return adapter
	})
}

func TestDirectoryAdapterPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	first, err := directory.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.CreateBucket(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Put(ctx, "alpha", "kept.txt", bytes.NewReader([]byte("durable")), persistence.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := directory.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	object, err := second.Get(ctx, "alpha", "kept.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	body, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "durable" {
		t.Fatalf("reopened body = %q", body)
	}
}
