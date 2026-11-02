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
		// A directory that cannot be listed is not an empty directory.
		if absence(err) {
			return nil
		}
		return uncertain(err)
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
		// A file this host cannot open is not a file that is not there. Omitting it
		// would make a workspace whose objects cannot be read look like one that is gone.
		if errors.Is(err, rooted.ErrUnsupported) {
			return err
		}
		if absence(err) {
			return nil
		}
		return uncertain(err)
	}
	file.Close()
	manifestEntry, _ := l.store.objectIndex.entry(l.bucket, key)
	if meta := l.store.metaFromEntry(l.bucket, key, manifestEntry, info); meta != nil {
		l.byKey[key] = *meta
	}
	return nil
}
