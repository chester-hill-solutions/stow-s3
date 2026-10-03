package rooted

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
)

// mutationParents refuses existing ancestor links; os.Root enforces confinement
// even if an ancestor changes after the check.
func (r *Root) mutationParents(relative string, create bool) error {
	if !Supported() {
		return ErrUnsupported
	}
	if err := r.Check(); err != nil {
		return err
	}
	if !filepath.IsLocal(relative) {
		return fmt.Errorf("rooted: path is not local")
	}
	current := "."
	for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(relative)), "/") {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := r.root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && create {
			if err := r.root.Mkdir(current, 0755); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = r.root.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("rooted: parent must be a real directory")
		}
	}
	return nil
}

func (r *Root) WriteAtomic(relative string, data []byte, perm os.FileMode) error {
	if err := r.mutationParents(relative, true); err != nil {
		return err
	}
	return atomicfile.WriteRoot(r.root, relative, data, perm)
}

// Remove unlinks a leaf without following it, including a symlink leaf.
func (r *Root) Remove(relative string) error {
	if err := r.mutationParents(relative, false); err != nil {
		return err
	}
	return r.root.Remove(relative)
}
