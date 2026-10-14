package atomicfile_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
)

type brokenAtomicReader struct{}

func (brokenAtomicReader) Read(data []byte) (int, error) {
	copy(data, "partial")
	return 7, io.ErrUnexpectedEOF
}
func TestStreamFailurePreservesDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "object")
	if err := atomicfile.Write(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteStream(path, 0644, []byte("header"), brokenAtomicReader{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("data %q %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("litter %v %v", entries, err)
	}
	if err := atomicfile.WriteStream(path, 0600, []byte("header"), strings.NewReader("payload")); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "headerpayload" {
		t.Fatalf("data %q %v", data, err)
	}
}
