package corpus_test

import (
	"testing"

	"github.com/chester-hill-solutions/stow-s3/conformance/corpus"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence/directory"
)

func TestRunAdapterAgainstTheDirectoryReference(t *testing.T) {
	corpus.RunAdapter(t, func(t *testing.T) persistence.Adapter {
		adapter, err := directory.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return adapter
	})
}
