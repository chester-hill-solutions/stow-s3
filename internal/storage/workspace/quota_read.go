package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type quotaStoreView struct{ *Store }

func (s *Store) QuotaStore() storage.Store { return quotaStoreView{Store: s} }

func (view quotaStoreView) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	return view.Store.headObjectForQuota(ctx, bucket, key)
}

func (s *Store) headObjectForQuota(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := storage.ValidateKey(key); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	if !s.bucketExists(bucket) {
		return nil, storage.ErrBucketNotFound
	}
	entry, indexed := s.objectIndex.entry(bucket, key)
	if !s.layout.IsNatural(key) && (!indexed || entry.Form != FormEscaped) {
		return nil, storage.ErrObjectNotFound
	}
	root, err := s.readRoot()
	if err != nil {
		return nil, uncertain(err)
	}
	defer root.Close()
	path := filepath.Join(s.bucketDir(bucket), filepath.FromSlash(key))
	if indexed && entry.Form == FormEscaped {
		path = EscapedPath(s.root, bucket, key)
	}
	relative, err := filepath.Rel(s.root, path)
	if err != nil {
		return nil, uncertain(err)
	}
	file, dir, err := openQuotaFile(root, filepath.ToSlash(relative))
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, uncertain(err)
	}
	return s.metaFromEntry(bucket, key, entry, info), nil
}

func openQuotaFile(root *rooted.Root, relative string) (*os.File, *os.File, error) {
	dir, _, err := openExactWorkspaceParent(root, relative)
	if err != nil {
		if errors.Is(err, storage.ErrWorkspaceParentInvalid) || errors.Is(err, os.ErrNotExist) {
			return nil, nil, storage.ErrObjectNotFound
		}
		return nil, nil, uncertain(err)
	}
	entries, err := dir.ReadDir(-1)
	if err != nil {
		dir.Close()
		return nil, nil, uncertain(err)
	}
	leaf := filepath.Base(relative)
	if !quotaLeafIsExact(entries, leaf) {
		dir.Close()
		return nil, nil, storage.ErrObjectNotFound
	}
	file, err := root.OpenRegularFile(filepath.FromSlash(relative))
	if err != nil {
		dir.Close()
		if errors.Is(err, rooted.ErrSymlink) || errors.Is(err, rooted.ErrNotRegular) || errors.Is(err, os.ErrNotExist) {
			return nil, nil, storage.ErrObjectNotFound
		}
		return nil, nil, uncertain(err)
	}
	parentInfo, err := dir.Stat()
	if err == nil && !sameWorkspaceReadDirectory(root, filepath.ToSlash(filepath.Dir(relative)), parentInfo) {
		err = storage.ErrObjectUncertain
	}
	if err != nil {
		file.Close()
		dir.Close()
		return nil, nil, uncertain(err)
	}
	return file, dir, nil
}

func quotaLeafIsExact(entries []os.DirEntry, leaf string) bool {
	exact, folded := false, false
	for _, entry := range entries {
		if entry.Name() == leaf {
			exact = true
		} else if strings.EqualFold(entry.Name(), leaf) {
			folded = true
		}
	}
	return exact && !folded
}
