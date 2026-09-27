package stow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func scanCheckpointFiles(root string, includeSensitive bool) ([]CheckpointFile, []string, int64, error) {
	scan := checkpointScan{root: root, includeSensitive: includeSensitive}
	err := filepath.WalkDir(root, scan.visit)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("stow: scan checkpoint inputs: %w", err)
	}
	sort.Slice(scan.excluded, func(i, j int) bool { return scan.excluded[i] < scan.excluded[j] })
	return scan.files, scan.excluded, scan.total, nil
}

type checkpointScan struct {
	root             string
	includeSensitive bool
	files            []CheckpointFile
	excluded         []string
	total            int64
}

func (s *checkpointScan) visit(path string, entry os.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if checkpointMetadataPath(s.root, path, entry) {
		return skipCheckpointMetadata(entry)
	}
	if path == s.root || entry.IsDir() {
		return nil
	}
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if !s.includeSensitive && sensitiveSeedPath(rel) {
		s.excluded = append(s.excluded, rel)
		return nil
	}
	return s.addRegularFile(path, rel, entry)
}

func checkpointMetadataPath(root, path string, entry os.DirEntry) bool {
	return (filepath.Dir(path) == root && strings.EqualFold(entry.Name(), ".stow")) || strings.EqualFold(entry.Name(), ".git")
}

func skipCheckpointMetadata(entry os.DirEntry) error {
	if entry.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

func (s *checkpointScan) addRegularFile(path, rel string, entry os.DirEntry) error {
	if !validCheckpointPath(rel) {
		return fmt.Errorf("stow: checkpoint path is not portable: %q", rel)
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("stow: checkpoint refuses symbolic link %q", rel)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("stow: checkpoint supports regular files only: %q", rel)
	}
	digest, size, err := digestFile(path)
	if err != nil {
		return err
	}
	if size > int64(^uint64(0)>>1)-s.total {
		return fmt.Errorf("stow: checkpoint byte count exceeds supported limit")
	}
	s.files = append(s.files, CheckpointFile{Path: rel, Size: size, Mode: uint32(info.Mode().Perm()), SHA256: digest})
	s.total += size
	return nil
}

func copyCheckpointFiles(ctx context.Context, root, target string, files []CheckpointFile) error {
	for _, item := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		source := filepath.Join(root, filepath.FromSlash(item.Path))
		destination := filepath.Join(target, "files", filepath.FromSlash(item.Path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		in, err := os.Open(source)
		if err != nil {
			return fmt.Errorf("stow: read checkpoint input %q: %w", item.Path, err)
		}
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(item.Mode)&0o755)
		if err != nil {
			_ = in.Close()
			return err
		}
		hash := sha256.New()
		size, copyErr := io.Copy(io.MultiWriter(out, hash), in)
		inErr, outErr := in.Close(), out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		if outErr != nil {
			return outErr
		}
		if size != item.Size || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
			return fmt.Errorf("stow: checkpoint input %q changed during capture", item.Path)
		}
	}
	return nil
}

func digestFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", 0, copyErr
	}
	if closeErr != nil {
		return "", 0, closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}
