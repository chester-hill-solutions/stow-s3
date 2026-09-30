package rooted_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
)

// Ties the predicate to the behaviour rather than to a build tag, so the two cannot
// drift: a caller asking Supported() must get the answer the open gives, including
// ErrUnsupported rather than a refusal it cannot recognise.
func TestSupportedAgreesWithWhatThisHostCanDo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ordinary.txt"), []byte("ordinary"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := rooted.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	file, err := root.OpenRegularFile("ordinary.txt")
	if file != nil {
		file.Close()
	}

	switch {
	case rooted.Supported():
		if err != nil {
			t.Fatalf("Supported() is true but the open failed: %v", err)
		}
	case !errors.Is(err, rooted.ErrUnsupported):
		t.Fatalf("Supported() is false but the open reported %v rather than ErrUnsupported", err)
	}
}
