package fs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type failedPrefixReader struct{ hint int }

func (r failedPrefixReader) Len() int { return r.hint }
func (failedPrefixReader) Read(data []byte) (int, error) {
	return copy(data, "partial"), io.ErrUnexpectedEOF
}

func TestPrefixFullReadRetainsUnderlyingFailure(t *testing.T) {
	s := stagedStore(t)
	ctx := context.Background()
	original, err := s.PutObject(ctx, "assets", "key", bytes.NewBufferString("original"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, hint := range []int{0, 6, 64 << 10} {
		if _, err := s.PutObject(ctx, "assets", "key", failedPrefixReader{hint: hint}, storage.PutOptions{}); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("hint %d discarded source failure: %v", hint, err)
		}
		meta, err := s.HeadObject(ctx, "assets", "key")
		if err != nil || meta.VersionID != original.VersionID {
			t.Fatalf("hint %d replaced published object: %+v %v", hint, meta, err)
		}
	}
	assertNoStageFiles(t, s)
}
