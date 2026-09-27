package stow

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	checkpointArchiveVersion      = 1
	maxCheckpointManifestSize     = 16 << 20
	maxCheckpointArchiveTrailer   = 1 << 20
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

// CheckpointArchivePreview describes the verified contents of an archive.
// Sensitive-looking paths are reported so callers can make an informed choice;
// preview itself does not import or extract data and does not require opt-in.
type CheckpointArchivePreview struct {
	CheckpointID   string           `json:"checkpoint_id"`
	WorkspaceID    string           `json:"workspace_id"`
	ParentID       string           `json:"parent_checkpoint_id,omitempty"`
	Files          []CheckpointFile `json:"files"`
	Excluded       []string         `json:"excluded_sensitive_paths,omitempty"`
	SensitivePaths []string         `json:"sensitive_paths,omitempty"`
	Bytes          int64            `json:"bytes"`
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
	if err := validateCheckpointArchiveOptions(options); err != nil {
		return err
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

// PreviewCheckpointArchive validates archive structure, file paths, limits,
// content digests, and the gzip trailer without extracting or publishing files.
// The returned paths are sorted for stable machine-readable output.
func PreviewCheckpointArchive(ctx context.Context, input io.Reader, options CheckpointArchiveOptions) (CheckpointArchivePreview, error) {
	if input == nil {
		return CheckpointArchivePreview{}, fmt.Errorf("stow: checkpoint archive input is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateCheckpointArchiveOptions(options); err != nil {
		return CheckpointArchivePreview{}, err
	}
	gz, tr, manifest, err := readCheckpointArchive(ctx, input)
	if err != nil {
		return CheckpointArchivePreview{}, err
	}
	defer gz.Close()
	if int64(len(manifest.Files)) > archiveFileLimit(options.MaxFiles) {
		return CheckpointArchivePreview{}, fmt.Errorf("stow: checkpoint file count %d exceeds preview limit %d", len(manifest.Files), archiveFileLimit(options.MaxFiles))
	}
	bytes := checkpointManifestBytes(manifest)
	if bytes > archiveByteLimit(options.MaxBytes) {
		return CheckpointArchivePreview{}, fmt.Errorf("stow: checkpoint bytes %d exceed preview limit %d", bytes, archiveByteLimit(options.MaxBytes))
	}
	if err := verifyCheckpointArchiveFiles(ctx, tr, manifest); err != nil {
		return CheckpointArchivePreview{}, err
	}
	if err := verifyCheckpointArchiveTrailer(gz); err != nil {
		return CheckpointArchivePreview{}, err
	}
	files := append([]CheckpointFile(nil), manifest.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sensitive := make([]string, 0)
	for _, file := range files {
		if sensitiveSeedPath(file.Path) {
			sensitive = append(sensitive, file.Path)
		}
	}
	return CheckpointArchivePreview{
		CheckpointID: manifest.ID, WorkspaceID: manifest.WorkspaceID,
		ParentID: manifest.ParentID, Files: files,
		Excluded:       append([]string(nil), manifest.Excluded...),
		SensitivePaths: sensitive, Bytes: bytes,
	}, nil
}

func verifyCheckpointArchiveFiles(ctx context.Context, tr *tar.Reader, manifest CheckpointManifest) error {
	expected := make(map[string]CheckpointFile, len(manifest.Files))
	for _, file := range manifest.Files {
		expected["files/"+file.Path] = file
	}
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
		digest := sha256.New()
		if _, err := io.CopyN(digest, contextReader{ctx: ctx, reader: tr}, file.Size); err != nil {
			return fmt.Errorf("stow: read checkpoint archive file %q: %w", file.Path, err)
		}
		if hex.EncodeToString(digest.Sum(nil)) != file.SHA256 {
			return fmt.Errorf("stow: checkpoint archive file %q failed integrity validation", file.Path)
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("stow: checkpoint archive is missing files")
	}
	return nil
}

func validateCheckpointArchiveOptions(options CheckpointArchiveOptions) error {
	if options.MaxBytes < 0 || options.MaxFiles < 0 {
		return fmt.Errorf("stow: checkpoint archive limits must not be negative")
	}
	return nil
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
		pathInfo, err := os.Lstat(path)
		if err != nil || !pathInfo.Mode().IsRegular() {
			return fmt.Errorf("stow: checkpoint file %q failed integrity validation", file.Path)
		}
		input, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("stow: open checkpoint file %q: %w", file.Path, err)
		}
		openedInfo, statErr := input.Stat()
		if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) || openedInfo.Size() != file.Size {
			_ = input.Close()
			return fmt.Errorf("stow: checkpoint file %q changed before export", file.Path)
		}
		hash := sha256.New()
		err = writeArchiveEntry(tw, "files/"+file.Path, file.Size, io.TeeReader(contextReader{ctx: ctx, reader: input}, hash))
		closeErr := input.Close()
		if err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
			return fmt.Errorf("stow: checkpoint file %q changed during export", file.Path)
		}
		if closeErr != nil {
			return closeErr
		}
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
