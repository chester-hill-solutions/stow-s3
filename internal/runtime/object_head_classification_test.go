package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type objectSizeLookup struct {
	storage.Store
	meta *storage.ObjectMeta
	err  error
}

func (s *objectSizeLookup) HeadObject(context.Context, string, string) (*storage.ObjectMeta, error) {
	return s.meta, s.err
}

func objectHeadAmbiguity(order string) error {
	unavailable := errors.New("head observation unavailable")
	err := errors.Join(storage.ErrMutationCommitted, storage.ErrObjectNotFound, unavailable)
	if order == "missing-first" || order == "wrapped-missing-first" {
		err = errors.Join(storage.ErrObjectNotFound, unavailable, storage.ErrMutationCommitted)
	}
	if order == "wrapped-marker-first" || order == "wrapped-missing-first" {
		return fmt.Errorf("head: %w", err)
	}
	return err
}

func TestStoredObjectSizeHeadClassification(t *testing.T) {
	unknown := errors.New("head unavailable")
	cases := []struct {
		name    string
		meta    *storage.ObjectMeta
		err     error
		missing bool
	}{
		{name: "present", meta: &storage.ObjectMeta{Size: 3}},
		{name: "missing", err: storage.ErrObjectNotFound, missing: true},
		{name: "wrapped-missing", err: fmt.Errorf("head: %w", storage.ErrObjectNotFound), missing: true},
		{name: "unknown", err: unknown},
		{name: "marker-only", err: errors.Join(storage.ErrMutationCommitted, unknown)},
		{name: "marker-first", err: objectHeadAmbiguity("marker-first")},
		{name: "missing-first", err: objectHeadAmbiguity("missing-first")},
		{name: "wrapped-marker-first", err: objectHeadAmbiguity("wrapped-marker-first")},
		{name: "wrapped-missing-first", err: objectHeadAmbiguity("wrapped-missing-first")},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			size, exists, err := storedObjectSize(context.Background(), &objectSizeLookup{meta: item.meta, err: item.err}, "probe", "target")
			if item.meta != nil {
				if size != 3 || !exists || err != nil {
					t.Fatalf("present: size=%d exists=%t error=%v", size, exists, err)
				}
				return
			}
			wantErr := item.err
			if item.missing {
				wantErr = nil
			}
			if size != 0 || exists || err != wantErr {
				t.Fatalf("observation: size=%d exists=%t error=%v want=%v", size, exists, err, wantErr)
			}
		})
	}
}
