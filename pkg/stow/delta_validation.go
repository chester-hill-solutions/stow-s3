package stow

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Validate at every public boundary, including direct in-memory application.
func validateDeltaDocument(d *DeltaDocument, allowSensitive bool) error {
	if err := validateDeltaHeader(d); err != nil {
		return err
	}
	size, err := validateDeltaContents(d, allowSensitive)
	if err != nil {
		return err
	}
	if size != d.Bytes || d.Files != int64(len(d.Changes)) {
		return errors.New("stow: delta content or totals disagree with changes")
	}
	return nil
}

func validateDeltaHeader(d *DeltaDocument) error {
	if d == nil {
		return errors.New("stow: nil delta")
	}
	if d.Version != DeltaVersion {
		return ErrDeltaVersionUnsupported
	}
	if !validCheckpointID(d.BaseID) || !validCheckpointID(d.TargetID) {
		return errors.New("stow: invalid delta checkpoint IDs")
	}
	if len(d.Changes) > defaultDeltaFiles {
		return errors.New("stow: delta exceeds file limit")
	}
	return nil
}

func validateDeltaContents(d *DeltaDocument, allowSensitive bool) (int64, error) {
	seen := make(map[string]bool, len(d.Changes))
	referenced := make(map[string]bool, len(d.Content))
	var size int64
	for _, change := range d.Changes {
		if !validCheckpointPath(change.Path) || seen[change.Path] {
			return 0, fmt.Errorf("stow: unsafe or repeated delta path %q", change.Path)
		}
		seen[change.Path] = true
		if err := validateDeltaChange(change); err != nil {
			return 0, err
		}
		data, present := d.Content[change.Path]
		if change.To != nil {
			referenced[change.Path] = true
		}
		if change.To == nil {
			if present {
				return 0, fmt.Errorf("stow: deleted path %q carries content", change.Path)
			}
			continue
		}
		if sensitiveSeedPath(change.Path) && (!allowSensitive || !d.IncludeSensitive) {
			return 0, fmt.Errorf("stow: sensitive delta path %q requires opt-in", change.Path)
		}
		if !present {
			return 0, fmt.Errorf("stow: delta carries no content for %q", change.Path)
		}
		if err := verifyCheckpointBytes(data, *change.To); err != nil {
			return 0, err
		}
		size += int64(len(data))
		if size > maxDeltaBytes {
			return 0, errors.New("stow: delta exceeds content limit")
		}
	}
	if err := validateNoUnreferencedDeltaContent(d, referenced); err != nil {
		return 0, err
	}
	return size, nil
}

func validateNoUnreferencedDeltaContent(d *DeltaDocument, referenced map[string]bool) error {
	for path := range d.Content {
		if !referenced[path] {
			return fmt.Errorf("stow: unreferenced delta content %q", path)
		}
	}
	return nil
}

func validateDeltaChange(c DeltaChange) error {
	valid := false
	switch c.Kind {
	case DeltaChangeAdded:
		valid = c.From == nil && c.To != nil
	case DeltaChangeDeleted:
		valid = c.From != nil && c.To == nil
	case DeltaChangeChanged:
		valid = c.From != nil && c.To != nil
	}
	if !valid {
		return fmt.Errorf("stow: invalid delta change %q", c.Path)
	}
	for _, file := range []*CheckpointFile{c.From, c.To} {
		if file != nil && (file.Path != c.Path || !validCheckpointFileMetadata(*file) || file.Mode & ^uint32(0o777) != 0) {
			return fmt.Errorf("stow: invalid delta metadata for %q", c.Path)
		}
	}
	return nil
}

func verifyCheckpointBytes(data []byte, file CheckpointFile) error {
	if int64(len(data)) != file.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != file.SHA256 {
		return fmt.Errorf("stow: checkpoint content %q disagrees with manifest", file.Path)
	}
	return nil
}

func readVerifiedCheckpointFile(dir string, file CheckpointFile) ([]byte, error) {
	root := filepath.Join(dir, "files")
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("stow: invalid checkpoint files directory")
	}
	current := root
	for _, segment := range strings.Split(file.Path, "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("stow: checkpoint path %q contains a symlink", file.Path)
		}
		if current != filepath.Join(root, filepath.FromSlash(file.Path)) && !info.IsDir() {
			return nil, fmt.Errorf("stow: checkpoint path %q has a non-directory parent", file.Path)
		}
	}
	info, err := os.Lstat(current)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != file.Size {
		return nil, fmt.Errorf("stow: invalid checkpoint file %q", file.Path)
	}
	f, err := os.Open(current)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, file.Size+1))
	if err != nil {
		return nil, err
	}
	if err := verifyCheckpointBytes(data, file); err != nil {
		return nil, err
	}
	return data, nil
}
