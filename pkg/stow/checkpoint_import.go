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
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// ImportCheckpoint validates a portable checkpoint archive in a private staging
// directory and atomically publishes it only after every path and digest passes.
func ImportCheckpoint(ctx context.Context, registryDir string, input io.Reader, options CheckpointArchiveOptions) (CheckpointInfo, error) {
	if input == nil {
		return CheckpointInfo{}, fmt.Errorf("stow: checkpoint archive input is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateCheckpointArchiveOptions(options); err != nil {
		return CheckpointInfo{}, err
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
	if err := extractCheckpointArchiveFiles(ctx, tr, stage, expected); err != nil {
		return CheckpointInfo{}, err
	}
	if err := verifyCheckpointArchiveTrailer(gz); err != nil {
		return CheckpointInfo{}, err
	}
	if _, err := verifyCheckpointReceiptPayload(ctx, stage, manifest); err != nil {
		return CheckpointInfo{}, err
	}
	if err := publishImportedCheckpoint(ctx, stage, checkpointRoot, manifest); err != nil {
		return CheckpointInfo{}, err
	}
	return CheckpointInfo{Version: manifest.Version, Objects: int64(len(manifest.Objects)), ID: manifest.ID, WorkspaceID: manifest.WorkspaceID, ParentID: manifest.ParentID, Created: manifest.Created, Files: int64(len(manifest.Files)), Bytes: manifestBytes, Excluded: append([]string(nil), manifest.Excluded...)}, nil
}

func verifyCheckpointArchiveTrailer(gz *gzip.Reader) error {
	trailer, err := io.ReadAll(io.LimitReader(gz, maxCheckpointArchiveTrailer+1))
	if err != nil {
		return fmt.Errorf("stow: verify checkpoint archive trailer: %w", err)
	}
	if len(trailer) > maxCheckpointArchiveTrailer {
		return fmt.Errorf("stow: checkpoint archive has an oversized trailer")
	}
	for _, value := range trailer {
		if value != 0 {
			return fmt.Errorf("stow: checkpoint archive has unexpected data after its tar entries")
		}
	}
	return nil
}

func readCheckpointArchive(ctx context.Context, input io.Reader) (*gzip.Reader, *tar.Reader, CheckpointManifest, error) {
	gz, err := gzip.NewReader(contextReader{ctx: ctx, reader: input})
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
	decoder := json.NewDecoder(bytes.NewReader(manifestData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&archive); err != nil {
		_ = gz.Close()
		return nil, nil, CheckpointManifest{}, fmt.Errorf("stow: decode checkpoint archive manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		_ = gz.Close()
		return nil, nil, CheckpointManifest{}, fmt.Errorf("stow: trailing checkpoint archive manifest data")
	}
	manifest := archive.Manifest
	if archive.FormatVersion != manifest.Version || !supportedCheckpointVersion(manifest.Version) || !validCheckpointID(manifest.ID) {
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
	if int64(len(checkpointPayloadFiles(manifest))) > archiveFileLimit(options.MaxFiles) {
		return nil, 0, fmt.Errorf("stow: checkpoint file count %d exceeds import limit %d", len(manifest.Files), archiveFileLimit(options.MaxFiles))
	}
	bytes := checkpointManifestBytes(manifest)
	if bytes > archiveByteLimit(options.MaxBytes) {
		return nil, 0, fmt.Errorf("stow: checkpoint bytes %d exceed import limit %d", bytes, archiveByteLimit(options.MaxBytes))
	}
	expected := checkpointPayloadFiles(manifest)
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
	relative := strings.TrimPrefix(strings.TrimPrefix(name, "files/"), "objects/")
	if !validCheckpointPath(relative) {
		return fmt.Errorf("stow: unsafe checkpoint archive path %q", name)
	}
	destination := filepath.Join(stage, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(file.Mode)&0o777)
	if err != nil {
		return err
	}
	_, copyErr := io.CopyN(output, contextReader{ctx: ctx, reader: input}, file.Size)
	if copyErr == nil {
		copyErr = output.Chmod(os.FileMode(file.Mode) & 0o777)
	}
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
	registryDir := filepath.Dir(checkpointRoot)
	lock, err := workspace.AcquireMutationCapture(registryDir, manifest.WorkspaceID)
	if err != nil {
		return err
	}
	defer lock.Release()
	if _, err := os.Lstat(filepath.Join(checkpointRoot, manifest.ID)); err == nil {
		return fmt.Errorf("stow: checkpoint %s already exists", manifest.ID)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := checkRegistryCheckpointAdmission(registryDir, manifest.WorkspaceID, checkpointManifestBytes(manifest)); err != nil {
		return err
	}
	return publishCheckpointContext(ctx, stage, checkpointRoot, manifest.ID, manifest)
}
