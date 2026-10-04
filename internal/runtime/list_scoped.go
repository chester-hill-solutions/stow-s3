package runtime

import (
	"context"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (i *Instance) listObjectsScoped(ctx context.Context, scope listScope, bucket string, query storage.ListOptions) (*storage.ListResult, error) {
	if scope.set == nil {
		return i.store.ListObjectsV2(ctx, bucket, query)
	}
	if query.MaxKeys > 1000 {
		query.MaxKeys = 1000
	}
	visibleQuery := query
	query.MaxKeys = 1000
	if query.ContinuationToken != "" {
		query.StartAfter = query.ContinuationToken
		query.ContinuationToken = ""
	}
	var visible []storage.ObjectMeta
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := i.store.ListObjectsV2(ctx, bucket, query)
		if err != nil {
			return nil, err
		}
		visible = append(visible, scope.visibleEntries(page)...)
		if len(visible) > visibleQuery.MaxKeys || !page.IsTruncated {
			return storage.PaginateObjects(visible, visibleQuery), nil
		}
		if page.NextContinuationToken == "" || page.NextContinuationToken == query.ContinuationToken {
			return nil, storage.ErrInvalidKey
		}
		query.ContinuationToken = page.NextContinuationToken
		query.StartAfter = ""
	}
}

func (s listScope) visibleEntries(page *storage.ListResult) []storage.ObjectMeta {
	var entries []storage.ObjectMeta
	for _, object := range page.Objects {
		if s.allows(object.Key) {
			entries = append(entries, object)
		}
	}
	for _, prefix := range page.CommonPrefixes {
		if s.allows(prefix) {
			entries = append(entries, storage.ObjectMeta{Key: prefix})
		}
	}
	return entries
}
