package stow

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	checkpointArchiveVersion      = 1
	maxCheckpointManifestSize     = 16 << 20
	defaultCheckpointArchiveBytes = 1 << 30
	defaultCheckpointArchiveFiles = 100_000
)

// CheckpointArchiveOptions bounds archive work. Zero limits use conservative
// defaults; callers can raise them for deliberately large workspaces.
type CheckpointArchiveOptions struct {
	MaxBytes              int64
	MaxFiles              int64
	IncludeSensitiveFiles bool
}

type checkpointArchiveHeader struct {
	FormatVersion int                `json:"format_version"`
	Manifest      CheckpointManifest `json:"manifest"`
}

// ExportCheckpoint writes a portable gzip-compressed tar archive. It includes
// only checkpoint bytes and metadata, never workspace credentials or registry
// state. Sensitive-looking file paths require a separate explicit export opt-in.
func ExportCheckpoint(ctx context.Context, registryDir, id string, output io.Writer, options CheckpointArchiveOptions) error {
	if output == nil {
		return fmt.Errorf("stow: checkpoint archive output is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	manifest, err := LoadCheckpoint(registryDir, id)
	if err != nil {
		return err
	}
	if int64(len(manifest.Files)) > archiveFileLimit(options.MaxFiles) {
		return fmt.Errorf("stow: checkpoint file count exceeds archive limit")
	}
	if err := validateArchiveSensitiveFiles(manifest, options.IncludeSensitiveFiles); err != nil {
		return err
	}
	checkpointDir, err := checkpointDirectory(registryDir, id)
	if err != nil {
		return err
	}
	total := checkpointManifestBytes(manifest)
	if total > archiveByteLimit(options.MaxBytes) {
		return fmt.Errorf("stow: checkpoint bytes %d exceed archive limit %d", total, archiveByteLimit(options.MaxBytes))
	}
	return writeCheckpointArchive(ctx, manifest, checkpointDir, output)
}

func writeCheckpointArchive(ctx context.Context, manifest CheckpointManifest, checkpointDir string, output io.Writer) error {
	gz := gzip.NewWriter(output)
	gz.Header.ModTime = time.Unix(0, 0).UTC()
	tw := tar.NewWriter(gz)
	writeHeader := checkpointArchiveHeader{FormatVersion: checkpointArchiveVersion, Manifest: manifest}
	manifestBytes, err := json.Marshal(writeHeader)
	if err != nil {
		return fmt.Errorf("stow: encode checkpoint archive manifest: %w", err)
	}
	if len(manifestBytes) > maxCheckpointManifestSize {
		return fmt.Errorf("stow: checkpoint manifest exceeds archive limit")
	}
	if err := writeArchiveEntry(tw, "manifest.json", int64(len(manifestBytes)), bytesReader(manifestBytes)); err != nil {
		return err
	}
	if err := writeCheckpointArchiveFiles(ctx, tw, manifest, checkpointDir); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("stow: finish checkpoint archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("stow: finish compressed checkpoint archive: %w", err)
	}
	return nil
}

func writeCheckpointArchiveFiles(ctx context.Context, tw *tar.Writer, manifest CheckpointManifest, checkpointDir string) error {
	files := append([]CheckpointFile(nil), manifest.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(checkpointDir, "files", filepath.FromSlash(file.Path))
		digest, size, err := digestFile(path)
		if err != nil || digest != file.SHA256 || size != file.Size {
			return fmt.Errorf("stow: checkpoint file %q failed integrity validation", file.Path)
		}
		input, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("stow: open checkpoint file %q: %w", file.Path, err)
		}
		err = writeArchiveEntry(tw, "files/"+file.Path, file.Size, contextReader{ctx: ctx, reader: input})
		closeErr := input.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// ImportCheckpoint validates a portable checkpoint archive in a private staging
// directory and atomically publishes it only after every path and digest passes.
func ImportCheckpoint(ctx context.Context, registryDir string, input io.Reader, options CheckpointArchiveOptions) (CheckpointInfo, error) {
	if input == nil {
		return CheckpointInfo{}, fmt.Errorf("stow: checkpoint archive input is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if registryDir == "" {
		var err error
		registryDir, err = DefaultWorkspaceRegistryDir()
		if err != nil {
			return CheckpointInfo{}, err
		}
	}
	if err := os.MkdirAll(registryDir, 0o700); err != nil {
		return CheckpointInfo{}, fmt.Errorf("stow: create checkpoint registry: %w", err)
	}
	checkpointRoot := filepath.Join(registryDir, "checkpoints")
	if err := os.MkdirAll(checkpointRoot, 0o700); err != nil {
		return CheckpointInfo{}, fmt.Errorf("stow: create checkpoint store: %w", err)
	}
	stage, err := os.MkdirTemp(checkpointRoot, ".import-")
	if err != nil {
		return CheckpointInfo{}, fmt.Errorf("stow: stage checkpoint import: %w", err)
	}
	defer os.RemoveAll(stage)

	gz, tr, manifest, err := readCheckpointArchive(ctx, input)
	if err != nil {
		return CheckpointInfo{}, err
	}
	defer gz.Close()
	expected, manifestBytes, err := validateImportedCheckpoint(manifest, options)
	if err != nil {
		return CheckpointInfo{}, err
	}
	if err := os.Mkdir(filepath.Join(stage, "files"), 0o700); err != nil {
		return CheckpointInfo{}, err
	}
	if err := extractCheckpointArchiveFiles(ctx, tr, stage, expected); err != nil {
		return CheckpointInfo{}, err
	}
	if err := verifyCheckpointArchiveTrailer(gz); err != nil {
		return CheckpointInfo{}, err
	}
	if err := publishImportedCheckpoint(ctx, stage, checkpointRoot, manifest); err != nil {
		return CheckpointInfo{}, err
	}
	return CheckpointInfo{ID: manifest.ID, WorkspaceID: manifest.WorkspaceID, ParentID: manifest.ParentID, Created: manifest.Created, Files: int64(len(manifest.Files)), Bytes: manifestBytes, Excluded: append([]string(nil), manifest.Excluded...)}, nil
}

func verifyCheckpointArchiveTrailer(gz *gzip.Reader) error {
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return fmt.Errorf("stow: verify checkpoint archive trailer: %w", err)
	}
	return nil
}

func readCheckpointArchive(ctx context.Context, input io.Reader) (*gzip.Reader, *tar.Reader, CheckpointManifest, error) {
	gz, err := gzip.NewReader(input)
	if err != nil {
		return nil, nil, CheckpointManifest{}, fmt.Errorf("stow: open checkpoint archive: %w", err)
	}
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	if err != nil {
		_ = gz.Close()
		return nil, nil, CheckpointManifest{}, fmt.Errorf("stow: read checkpoint archive manifest: %w", err)
	}
	if header.Name != "manifest.json" || header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > maxCheckpointManifestSize {
		_ = gz.Close()
		return nil, nil, CheckpointManifest{}, fmt.Errorf("stow: checkpoint archive must begin with a bounded manifest.json")
	}
	manifestData, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: tr}, maxCheckpointManifestSize+1))
	if err != nil {
		_ = gz.Close()
		return nil, nil, CheckpointManifest{}, fmt.Errorf("stow: read checkpoint archive manifest: %w", err)
	}
	if int64(len(manifestData)) != header.Size {
		_ = gz.Close()
		return nil, nil, CheckpointManifest{}, fmt.Errorf("stow: checkpoint archive manifest length does not match its header")
	}
	var archive checkpointArchiveHeader
	if err := json.Unmarshal(manifestData, &archive); err != nil {
		_ = gz.Close()
		return nil, nil, CheckpointManifest{}, fmt.Errorf("stow: decode checkpoint archive manifest: %w", err)
	}
	manifest := archive.Manifest
	if archive.FormatVersion != checkpointArchiveVersion || manifest.Version != checkpointVersion || !validCheckpointID(manifest.ID) {
		_ = gz.Close()
		return nil, nil, CheckpointManifest{}, fmt.Errorf("stow: unsupported or invalid checkpoint archive")
	}
	if err := validateCheckpointManifest(manifest); err != nil {
		_ = gz.Close()
		return nil, nil, CheckpointManifest{}, err
	}
	return gz, tr, manifest, nil
}

func validateImportedCheckpoint(manifest CheckpointManifest, options CheckpointArchiveOptions) (map[string]CheckpointFile, int64, error) {
	if err := validateArchiveSensitiveFiles(manifest, options.IncludeSensitiveFiles); err != nil {
		return nil, 0, err
	}
	if int64(len(manifest.Files)) > archiveFileLimit(options.MaxFiles) {
		return nil, 0, fmt.Errorf("stow: checkpoint file count %d exceeds import limit %d", len(manifest.Files), archiveFileLimit(options.MaxFiles))
	}
	bytes := checkpointManifestBytes(manifest)
	if bytes > archiveByteLimit(options.MaxBytes) {
		return nil, 0, fmt.Errorf("stow: checkpoint bytes %d exceed import limit %d", bytes, archiveByteLimit(options.MaxBytes))
	}
	expected := make(map[string]CheckpointFile, len(manifest.Files))
	for _, file := range manifest.Files {
		expected["files/"+file.Path] = file
	}
	return expected, bytes, nil
}

func extractCheckpointArchiveFiles(ctx context.Context, tr *tar.Reader, stage string, expected map[string]CheckpointFile) error {
	seen := make(map[string]struct{}, len(expected))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("stow: read checkpoint archive entry: %w", err)
		}
		file, ok := expected[header.Name]
		if !ok || header.Typeflag != tar.TypeReg || header.Size != file.Size {
			return fmt.Errorf("stow: unexpected or invalid checkpoint archive entry %q", header.Name)
		}
		if _, duplicate := seen[header.Name]; duplicate {
			return fmt.Errorf("stow: duplicate checkpoint archive entry %q", header.Name)
		}
		seen[header.Name] = struct{}{}
		if err := extractCheckpointFile(ctx, tr, stage, header.Name, file); err != nil {
			return err
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("stow: checkpoint archive is missing files")
	}
	return nil
}

func extractCheckpointFile(ctx context.Context, input io.Reader, stage, name string, file CheckpointFile) error {
	relative := strings.TrimPrefix(name, "files/")
	if !validCheckpointPath(relative) {
		return fmt.Errorf("stow: unsafe checkpoint archive path %q", name)
	}
	destination := filepath.Join(stage, "files", filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(file.Mode)&0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.CopyN(output, contextReader{ctx: ctx, reader: input}, file.Size)
	closeErr := output.Close()
	if copyErr != nil {
		return fmt.Errorf("stow: extract checkpoint file %q: %w", relative, copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	digest, size, err := digestFile(destination)
	if err != nil || digest != file.SHA256 || size != file.Size {
		return fmt.Errorf("stow: checkpoint archive file %q failed integrity validation", relative)
	}
	return nil
}

func publishImportedCheckpoint(ctx context.Context, stage, checkpointRoot string, manifest CheckpointManifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	target := filepath.Join(checkpointRoot, manifest.ID)
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("stow: checkpoint %s already exists", manifest.ID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		return fmt.Errorf("stow: publish imported checkpoint: %w", err)
	}
	return nil
}

func checkpointManifestBytes(manifest CheckpointManifest) int64 {
	var total int64
	for _, file := range manifest.Files {
		if file.Size > int64(^uint64(0)>>1)-total {
			return int64(^uint64(0) >> 1)
		}
		total += file.Size
	}
	return total
}

func validateArchiveSensitiveFiles(manifest CheckpointManifest, include bool) error {
	if include {
		return nil
	}
	for _, file := range manifest.Files {
		if sensitiveSeedPath(file.Path) {
			return fmt.Errorf("stow: checkpoint includes sensitive-looking path %q (explicit opt-in required)", file.Path)
		}
	}
	return nil
}

func archiveByteLimit(value int64) int64 {
	if value > 0 {
		return value
	}
	return defaultCheckpointArchiveBytes
}

func archiveFileLimit(value int64) int64 {
	if value > 0 {
		return value
	}
	return defaultCheckpointArchiveFiles
}

func writeArchiveEntry(output *tar.Writer, name string, size int64, input io.Reader) error {
	if err := output.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: time.Unix(0, 0).UTC(), Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
		return fmt.Errorf("stow: write checkpoint archive header %q: %w", name, err)
	}
	if _, err := io.CopyN(output, input, size); err != nil {
		return fmt.Errorf("stow: write checkpoint archive entry %q: %w", name, err)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func bytesReader(data []byte) io.Reader { return bytes.NewReader(data) }
