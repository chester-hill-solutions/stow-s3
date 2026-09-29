package stow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"golang.org/x/sync/errgroup"
)

func scanCheckpointFiles(root string, includeSensitive bool) ([]CheckpointFile, []string, int64, error) {
	return scanCheckpointFilesContext(context.Background(), root, includeSensitive)
}

func scanCheckpointFilesContext(ctx context.Context, root string, includeSensitive bool) ([]CheckpointFile, []string, int64, error) {
	source, err := rooted.Open(root)
	if err != nil {
		return nil, nil, 0, err
	}
	defer source.Close()
	return scanCheckpointRoot(ctx, source, includeSensitive)
}

func scanCheckpointRoot(ctx context.Context, source *rooted.Root, includeSensitive bool) ([]CheckpointFile, []string, int64, error) {
	scan := checkpointScan{ctx: ctx, root: ".", source: source, includeSensitive: includeSensitive}
	err := fs.WalkDir(source.FS(), ".", scan.visit)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("stow: scan checkpoint inputs: %w", err)
	}
	if err := source.Check(); err != nil {
		return nil, nil, 0, err
	}
	sort.Slice(scan.excluded, func(i, j int) bool { return scan.excluded[i] < scan.excluded[j] })
	return scan.files, scan.excluded, scan.total, nil
}

type checkpointScan struct {
	source           *rooted.Root
	ctx              context.Context
	root             string
	includeSensitive bool
	files            []CheckpointFile
	excluded         []string
	total            int64
}

func (s *checkpointScan) visit(path string, entry os.DirEntry, walkErr error) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
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
	input, err := s.source.OpenRegularFile(filepath.FromSlash(rel))
	if err != nil {
		return err
	}
	digest, size, err := digestCheckpointInput(s.ctx, input)
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

// Copy independent payloads with bounded buffers and descriptors. Wait for every
// worker before verification, publication, or the caller's staging cleanup.
func copyCheckpointFiles(ctx context.Context, source *rooted.Root, target string, files []CheckpointFile) error {
	group, pending := errgroup.WithContext(ctx)
	group.SetLimit(8)
	for _, item := range files {
		if pending.Err() != nil {
			break
		}
		group.Go(func() error { return copyCheckpointInput(pending, source, target, item) })
	}
	if err := group.Wait(); err != nil {
		return err
	}
	return ctx.Err()
}

func copyCheckpointInput(ctx context.Context, source *rooted.Root, target string, item CheckpointFile) error {
	destination := filepath.Join(target, "files", filepath.FromSlash(item.Path))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	in, err := source.OpenRegularFile(filepath.FromSlash(item.Path))
	if err != nil {
		return fmt.Errorf("stow: read checkpoint input %q: %w", item.Path, err)
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(item.Mode)&0o777)
	if err != nil {
		_ = in.Close()
		return err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(out, hash), io.LimitReader(checkpointReader{ctx: ctx, reader: in}, item.Size+1))
	if copyErr == nil {
		copyErr = out.Chmod(os.FileMode(item.Mode) & 0o777)
	}
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
		return checkpointFailure("workspace_changed", "copy", "not_committed", fmt.Errorf("checkpoint input %q changed during capture", item.Path))
	}
	return nil
}

func digestFile(path string) (string, int64, error) {
	return digestFileContext(context.Background(), path)
}

func digestFileContext(ctx context.Context, path string) (string, int64, error) {
	source, err := rooted.Open(filepath.Dir(path))
	if err != nil {
		return "", 0, err
	}
	defer source.Close()
	file, err := source.OpenRegularFile(filepath.Base(path))
	if err != nil {
		return "", 0, err
	}
	return digestCheckpointInput(ctx, file)
}

func digestCheckpointInput(ctx context.Context, file *os.File) (string, int64, error) {
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return "", 0, fmt.Errorf("stow: checkpoint input is not regular")
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, io.LimitReader(checkpointReader{ctx: ctx, reader: file}, info.Size()+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", 0, copyErr
	}
	if closeErr != nil {
		return "", 0, closeErr
	}
	if size != info.Size() {
		return "", 0, fmt.Errorf("stow: checkpoint input changed size while reading")
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

type checkpointReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r checkpointReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
