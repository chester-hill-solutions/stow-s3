package storage

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type hintedReader struct {
	io.Reader
	size int64
}

func (r hintedReader) ReadSize() int64 { return r.size }

func TestReadOwnedBytesSizeIsOnlyAnAllocationHint(t *testing.T) {
	for _, size := range []int64{-1, 0, 1, 128} {
		want := []byte("complete content")
		got, err := readOwnedBytes(hintedReader{Reader: bytes.NewReader(want), size: size})
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("hint=%d: %q %v", size, got, err)
		}
		want[0] = 'X'
		if got[0] == 'X' {
			t.Fatal("reader bytes were aliased")
		}
	}
}

type failedReader struct{}

func (failedReader) Read(p []byte) (int, error) { copy(p, "abc"); return 3, io.ErrUnexpectedEOF }

func TestReadOwnedBytesPreservesReadFailure(t *testing.T) {
	data, err := readOwnedBytes(failedReader{})
	if string(data) != "abc" || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %q, %v", data, err)
	}
}
