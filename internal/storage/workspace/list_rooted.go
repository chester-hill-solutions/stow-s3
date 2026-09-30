package workspace

import (
	"errors"
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
		// A file this host cannot open safely is omitted, since a listing including
		// it would describe bytes nothing verified. A host that cannot do the open
		// at all is the other case, and must not read as an empty workspace: that
		// is how a caller concludes its work was deleted.
		if errors.Is(err, rooted.ErrUnsupported) {
			return err
		}
		return nil
	}
	file.Close()
	manifestEntry, _ := l.store.objectIndex.entry(l.bucket, key)
	if meta := l.store.metaFromEntry(l.bucket, key, manifestEntry, info); meta != nil {
		l.byKey[key] = *meta
	}
	return nil
}
