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
	return copyCheckpointFilesFromParent(ctx, files, checkpointCopyPlan{source: source, target: target})
}

type checkpointCopyPlan struct {
	source         *rooted.Root
	target         string
	registryDir    string
	checkpointRoot string
	parentID       string
	parent         *CheckpointManifest
	parentFiles    map[string]CheckpointFile
}

func (plan *checkpointCopyPlan) loadParent() error {
	plan.checkpointRoot = filepath.Join(plan.registryDir, "checkpoints")
	parent := plan.parent
	var err error
	if parent == nil {
		parent, err = loadCheckpointParent(plan.registryDir, plan.parentID)
	}
	if err != nil {
		return err
	}
	plan.parentFiles = checkpointParentFiles(parent)
	return nil
}

func copyCheckpointFilesFromParent(ctx context.Context, files []CheckpointFile, plan checkpointCopyPlan) error {
	if err := plan.loadParent(); err != nil {
		return err
	}
	group, pending := errgroup.WithContext(ctx)
	group.SetLimit(8)
	for _, item := range files {
		group.Go(func() error {
			return copyCheckpointItem(pending, plan, item)
		})
	}
	return waitCheckpointCopies(group, ctx)
}

func copyCheckpointItem(ctx context.Context, plan checkpointCopyPlan, item CheckpointFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return plan.copy(ctx, item)
}

func (plan checkpointCopyPlan) copy(ctx context.Context, item CheckpointFile) error {
	if err := prepareCheckpointFileDestination(plan.target, item); err != nil {
		return err
	}
	parentFile, found := plan.parentFiles[item.Path]
	destination := filepath.Join(plan.target, "files", filepath.FromSlash(item.Path))
	destinationRelative, relErr := filepath.Rel(plan.checkpointRoot, destination)
	parentRelative := filepath.Join(plan.parentID, "files", filepath.FromSlash(item.Path))
	if plan.parent != nil {
		parentRelative = filepath.Join(plan.parent.ID, "files", filepath.FromSlash(item.Path))
	}
	if found && relErr == nil && checkpointFilesMatch(parentFile, item) && tryReuseCheckpointPayload(ctx, plan.checkpointRoot, parentRelative, destinationRelative, item) {
		return nil
	}
	return copyCheckpointInput(ctx, plan.source, plan.target, item)
}

func checkpointFilesMatch(parent, current CheckpointFile) bool {
	return parent.Size == current.Size && parent.Mode == current.Mode && parent.SHA256 == current.SHA256
}

func prepareCheckpointFileDestination(target string, item CheckpointFile) error {
	return os.MkdirAll(filepath.Dir(filepath.Join(target, "files", filepath.FromSlash(item.Path))), 0o700)
}

func checkpointParentFiles(parent *CheckpointManifest) map[string]CheckpointFile {
	parentFiles := make(map[string]CheckpointFile)
	if parent == nil {
		return parentFiles
	}
	for _, file := range parent.Files {
		parentFiles[file.Path] = file
	}
	return parentFiles
}

func waitCheckpointCopies(group *errgroup.Group, ctx context.Context) error {
	if err := group.Wait(); err != nil {
		return err
	}
	return ctx.Err()
}

func tryReuseCheckpointPayload(ctx context.Context, checkpointRoot, parentRelative, destinationRelative string, item CheckpointFile) bool {
	parentInfo, valid := checkpointParentPayloadMatches(ctx, checkpointRoot, parentRelative, item)
	if !valid {
		return false
	}
	return ctx.Err() == nil && linkVerifiedCheckpointPayload(checkpointRoot, parentRelative, destinationRelative, parentInfo)
}

func checkpointParentPayloadMatches(ctx context.Context, checkpointRoot, parentRelative string, item CheckpointFile) (os.FileInfo, bool) {
	parent, err := rooted.Open(checkpointRoot)
	if err != nil {
		return nil, false
	}
	defer parent.Close()
	input, err := parent.OpenRegularFile(parentRelative)
	if err != nil {
		return nil, false
	}
	info, err := input.Stat()
	if err != nil || info.Size() != item.Size || !checkpointStoredMode(info.Mode().Perm(), item.Mode) {
		_ = input.Close()
		return nil, false
	}
	digest, size, err := digestCheckpointInput(ctx, input)
	return info, err == nil && size == item.Size && digest == item.SHA256 && ctx.Err() == nil
}

func linkVerifiedCheckpointPayload(checkpointRoot, parentRelative, destinationRelative string, parentInfo os.FileInfo) bool {
	root, err := os.OpenRoot(checkpointRoot)
	if err != nil {
		return false
	}
	defer root.Close()
	if err := root.Link(parentRelative, destinationRelative); err != nil {
		return false
	}
	if verifyLinkedCheckpointPayload(checkpointRoot, destinationRelative, parentInfo) {
		return true
	}
	_ = root.Remove(destinationRelative)
	return false
}

func openLinkedCheckpointPayload(checkpointRoot, destinationRelative string) (*rooted.Root, *os.File, error) {
	root, err := rooted.Open(checkpointRoot)
	if err != nil {
		return nil, nil, err
	}
	file, err := root.OpenRegularFile(destinationRelative)
	if err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	return root, file, nil
}

func verifyLinkedCheckpointPayload(checkpointRoot, destinationRelative string, parentInfo os.FileInfo) bool {
	root, file, err := openLinkedCheckpointPayload(checkpointRoot, destinationRelative)
	if err != nil {
		return false
	}
	defer root.Close()
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(parentInfo, info) {
		return false
	}
	if err := file.Chmod(checkpointStoredPayloadMode); err != nil {
		return false
	}
	linkedInfo, err := file.Stat()
	if err != nil || linkedInfo.Mode().Perm() != checkpointStoredPayloadMode {
		return false
	}
	return true
}

const checkpointStoredPayloadMode os.FileMode = 0o400

func checkpointStoredMode(actual os.FileMode, manifestMode uint32) bool {
	return actual.Perm() == os.FileMode(manifestMode)&0o777 || actual.Perm() == checkpointStoredPayloadMode
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
		copyErr = out.Chmod(checkpointStoredPayloadMode)
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
