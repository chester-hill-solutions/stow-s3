package workspace

import (
	"io/fs"
	"path/filepath"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type naturalObjectListing struct {
	store         *Store
	bucket, start string
	source        *rooted.Root
	byKey         map[string]storage.ObjectMeta
}

func (l *naturalObjectListing) visit(path string, entry fs.DirEntry, err error) error {
	if err != nil {
		return nil
	}
	relative, err := filepath.Rel(l.start, path)
	if err != nil {
		return nil
	}
	relative = filepath.ToSlash(relative)
	if IsInternal(relative) {
		if entry.IsDir() {
			return fs.SkipDir
		}
		return nil
	}
	if entry.IsDir() {
		return nil
	}
	key, ok := KeyFromNaturalPath(relative)
	if !ok {
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return nil
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	file, err := l.source.OpenRegularFile(path)
	if err != nil {
		return nil
	}
	file.Close()
	manifestEntry, _ := l.store.objectIndex.entry(l.bucket, key)
	if meta := l.store.metaFromEntry(l.bucket, key, manifestEntry, info); meta != nil {
		l.byKey[key] = *meta
	}
	return nil
}
