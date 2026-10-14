package fs

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestRecordWriteToCopiesOnlyRemainingPayload(t *testing.T) {
	s := stagedStore(t)
	ctx := context.Background()
	if _, err := s.PutObject(ctx, "assets", "key", bytes.NewBufferString("0123456789"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	reader, _, err := s.GetObject(ctx, "assets", "key")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	seeker, ok := reader.(io.Seeker)
	if ok {
		if _, err := seeker.Seek(4, io.SeekStart); err != nil {
			t.Fatal(err)
		}
	} else if _, err := io.CopyN(io.Discard, reader, 4); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	n, err := io.Copy(&out, reader)
	if err != nil || n != 6 || out.String() != "456789" {
		t.Fatalf("remaining %q n=%d err=%v", out.String(), n, err)
	}
	if n, err := io.Copy(&out, reader); n != 0 || err != nil {
		t.Fatalf("EOF %d %v", n, err)
	}
	if !ok {
		return
	}
	if _, err := seeker.Seek(100, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if n, err := io.Copy(&out, reader); n != 0 || err != nil {
		t.Fatalf("past EOF %d %v", n, err)
	}
}
