package runthrough

// Merged listing: walking every page of a prefix, and unioning what the local and
// upstream stores hold.
//
// Split out of adapter.go because it is the listing concern rather than the
// adapter's, and because adapter.go had reached the file-size ceiling with these
// at its tail.

import (
	"context"
	"sort"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// listAllObjects walks every page for a prefix so merged listings can re-paginate stably.
func listAllObjects(ctx context.Context, store interface {
	ListObjectsV2(context.Context, string, storage.ListOptions) (*storage.ListResult, error)
}, bucket, prefix string) ([]storage.ObjectMeta, error) {
	var out []storage.ObjectMeta
	token := ""
	for {
		page, err := store.ListObjectsV2(ctx, bucket, storage.ListOptions{
			Prefix:            prefix,
			MaxKeys:           1000,
			ContinuationToken: token,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, page.Objects...)
		if !page.IsTruncated || page.NextContinuationToken == "" {
			return out, nil
		}
		token = page.NextContinuationToken
	}
}

// mergeObjectLists unions by key; local metadata wins on duplicates.
func mergeObjectLists(local, upstream []storage.ObjectMeta) []storage.ObjectMeta {
	byKey := make(map[string]storage.ObjectMeta, len(local)+len(upstream))
	for _, obj := range upstream {
		byKey[obj.Key] = obj
	}
	for _, obj := range local {
		byKey[obj.Key] = obj
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]storage.ObjectMeta, 0, len(keys))
	for _, k := range keys {
		out = append(out, byKey[k])
	}
	return out
}
