package stow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
)

type HandoffArchive struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Files  int64  `json:"files"`
}

type Handoff struct {
	Version      int             `json:"version"`
	WorkspaceID  string          `json:"workspace_id"`
	CheckpointID string          `json:"checkpoint_id,omitempty"`
	RegistryDir  string          `json:"registry_dir,omitempty"`
	Team         string          `json:"team,omitempty"`
	Archive      *HandoffArchive `json:"archive,omitempty"`
}

type HandoffExportOptions struct {
	RegistryDir    string
	WorkspaceID    string
	CheckpointID   string
	BundleDir      string
	ArchiveOptions CheckpointArchiveOptions
}

// ExportHandoff creates a new directory containing a self-contained portable bundle.
func ExportHandoff(ctx context.Context, options HandoffExportOptions) (Handoff, error) {
	registryDir, workspaceID, checkpointID, bundleDir := options.RegistryDir, options.WorkspaceID, options.CheckpointID, options.BundleDir
	if _, err := LookupWorkspace(registryDir, workspaceID); err != nil {
		return Handoff{}, err
	}
	manifest, err := LoadCheckpoint(registryDir, checkpointID)
	if err != nil {
		return Handoff{}, err
	}
	if manifest.WorkspaceID != workspaceID {
		return Handoff{}, errors.New("checkpoint belongs to a different workspace")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stage, err := os.MkdirTemp(filepath.Dir(bundleDir), ".stow-handoff-")
	if err != nil {
		return Handoff{}, err
	}
	defer os.RemoveAll(stage)
	bound, err := exportHandoffArchive(ctx, stage, options)
	if err != nil {
		return Handoff{}, err
	}
	document := Handoff{Version: 2, WorkspaceID: workspaceID, CheckpointID: checkpointID, Archive: bound}
	document.Archive.Files = int64(len(manifest.Files))
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return Handoff{}, err
	}
	if err := os.WriteFile(filepath.Join(stage, "handoff.json"), append(data, '\n'), 0600); err != nil {
		return Handoff{}, err
	}
	if err := syncCheckpointTree(ctx, stage); err != nil {
		return Handoff{}, err
	}
	if err := ctx.Err(); err != nil {
		return Handoff{}, err
	}
	if err := commitHandoff(stage, bundleDir, syncCheckpointPath); err != nil {
		var uncertain *HandoffError
		if errors.As(err, &uncertain) {
			return document, err
		}
		return Handoff{}, err
	}
	return document, nil
}

func ReadHandoff(path string) (Handoff, error) {
	file, err := openHandoffFile(path)
	if err != nil {
		return Handoff{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Handoff{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return Handoff{}, errors.New("handoff reference must be a regular file of at most 1 MiB")
	}
	return ReadHandoffReader(file)
}

func ReadHandoffReader(reader io.Reader) (Handoff, error) {
	data, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil {
		return Handoff{}, err
	}
	if len(data) > 1<<20 {
		return Handoff{}, errors.New("handoff reference exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document Handoff
	if err := decoder.Decode(&document); err != nil {
		return document, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return document, errors.New("handoff reference must contain exactly one JSON value")
	}
	return document, ValidateHandoff(document)
}

func ValidateHandoff(document Handoff) error {
	if document.Version != 1 && document.Version != 2 {
		return fmt.Errorf("unsupported handoff reference version %d", document.Version)
	}
	if document.WorkspaceID == "" {
		return errors.New("invalid handoff reference: no workspace id")
	}
	if document.Version == 1 && document.RegistryDir == "" {
		return errors.New("invalid handoff reference: no registry directory")
	}
	if document.Archive != nil && document.Archive.SHA256 == "" {
		return errors.New("invalid handoff reference: the archive has no digest")
	}
	return nil
}

// ImportHandoff verifies identity and a private copy of the received archive before import.
func ImportHandoff(ctx context.Context, path, registryDir string, document Handoff, options CheckpointArchiveOptions) (CheckpointInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateCheckpointArchiveOptions(options); err != nil {
		return CheckpointInfo{}, err
	}
	if err := ValidateHandoff(document); err != nil {
		return CheckpointInfo{}, err
	}
	archive, err := verifiedHandoffArchive(ctx, path, document.Archive, options)
	if err != nil {
		return CheckpointInfo{}, err
	}
	return importVerifiedHandoff(ctx, registryDir, document, archive, options)
}

func importVerifiedHandoff(ctx context.Context, registryDir string, document Handoff, archive *os.File, options CheckpointArchiveOptions) (CheckpointInfo, error) {
	defer func() { archive.Close(); os.Remove(archive.Name()) }()
	preview, err := PreviewCheckpointArchive(ctx, archive, options)
	if err != nil {
		return CheckpointInfo{}, err
	}
	if preview.WorkspaceID != document.WorkspaceID || (document.CheckpointID != "" && preview.CheckpointID != document.CheckpointID) {
		return CheckpointInfo{}, errors.New("handoff identity does not match its archive")
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return CheckpointInfo{}, err
	}
	return ImportCheckpoint(ctx, registryDir, archive, options)
}

// AdoptHandoff returns imported checkpoint information even if restoration fails.
func AdoptHandoff(ctx context.Context, path string, options WorkspaceOptions, archiveOptions CheckpointArchiveOptions) (*Workspace, CheckpointInfo, error) {
	document, err := ReadHandoff(path)
	if err != nil {
		return nil, CheckpointInfo{}, err
	}
	return adoptHandoff(document, options, func(registry string) (CheckpointInfo, error) {
		return ImportHandoff(ctx, path, registry, document, archiveOptions)
	})
}

// AdoptHandoffArchive consumes the checked document and opened archive without reopening paths.
func AdoptHandoffArchive(ctx context.Context, document Handoff, archive *os.File, options WorkspaceOptions, archiveOptions CheckpointArchiveOptions) (*Workspace, CheckpointInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateCheckpointArchiveOptions(archiveOptions); err != nil {
		return nil, CheckpointInfo{}, err
	}
	return adoptHandoff(document, options, func(registry string) (CheckpointInfo, error) {
		copy, err := verifiedHandoffSource(ctx, archive, document.Archive, archiveOptions)
		if err != nil {
			return CheckpointInfo{}, err
		}
		return importVerifiedHandoff(ctx, registry, document, copy, archiveOptions)
	})
}

func adoptHandoff(document Handoff, options WorkspaceOptions, importArchive func(string) (CheckpointInfo, error)) (*Workspace, CheckpointInfo, error) {
	if err := ValidateHandoff(document); err != nil {
		return nil, CheckpointInfo{}, err
	}
	if _, err := validatePrepareRoot(options.Dir); err != nil {
		return nil, CheckpointInfo{}, err
	}
	if options.MaxBytes < 0 || options.MaxObjects < 0 || options.MaxCheckpoints < 0 || options.MaxCheckpointBytes < 0 {
		return nil, CheckpointInfo{}, errors.New("handoff workspace limits must not be negative")
	}
	if document.Team != "" && options.Team != "" && document.Team != options.Team {
		return nil, CheckpointInfo{}, errors.New("handoff team conflicts with destination team")
	}
	if options.Team == "" {
		options.Team = document.Team
	}
	registry, err := ResolveRegistryDir(options.RegistryDir, options.Team)
	if err != nil {
		return nil, CheckpointInfo{}, err
	}
	imported, err := importArchive(registry)
	if err != nil {
		return nil, imported, err
	}
	options.RegistryDir, options.Team, options.Adopted = registry, "", true
	ws, err := RestoreCheckpoint(registry, imported.ID, options)
	if err != nil {
		return nil, imported, fmt.Errorf("checkpoint %s imported; destination restore failed: %w", imported.ID, err)
	}
	return ws, imported, nil
}

func verifiedHandoffArchive(ctx context.Context, documentPath string, bound *HandoffArchive, options CheckpointArchiveOptions) (_ *os.File, resultErr error) {
	if bound == nil {
		return nil, errors.New("this handoff is a same-machine reference with no archive; export one and hand it off with --archive")
	}
	if bound.Path == "" {
		return nil, errors.New("handoff archive path is empty")
	}
	path := bound.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(documentPath), path)
	}
	source, err := openHandoffFile(path)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	return verifiedHandoffSource(ctx, source, bound, options)
}

func verifiedHandoffSource(ctx context.Context, source *os.File, bound *HandoffArchive, options CheckpointArchiveOptions) (_ *os.File, resultErr error) {
	if bound == nil || source == nil {
		return nil, errors.New("handoff archive is required")
	}
	info, err := source.Stat()
	if err != nil {
		return nil, err
	}
	if err := validateHandoffArchiveSize(info, options); err != nil {
		return nil, err
	}
	if bound.Bytes > 0 && info.Size() != bound.Bytes {
		return nil, errors.New("handoff archive size does not match reference")
	}
	copy, err := os.CreateTemp("", "stow-handoff-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			copy.Close()
			os.Remove(copy.Name())
		}
	}()
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(copy, digest), io.LimitReader(contextReader{ctx: ctx, reader: source}, info.Size()+1))
	if err != nil {
		return nil, err
	}
	if size != info.Size() || hex.EncodeToString(digest.Sum(nil)) != bound.SHA256 {
		return nil, errors.New("handoff archive digest does not match reference")
	}
	if _, err := copy.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return copy, nil
}

func exportHandoffArchive(ctx context.Context, stage string, options HandoffExportOptions) (*HandoffArchive, error) {
	archivePath := filepath.Join(stage, "checkpoint.tar.gz")
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	err = ExportCheckpoint(ctx, options.RegistryDir, options.CheckpointID, io.MultiWriter(archive, digest), options.ArchiveOptions)
	if err == nil {
		err = archive.Sync()
	}
	info, statErr := archive.Stat()
	closeErr := archive.Close()
	if err = errors.Join(err, statErr, closeErr); err != nil {
		return nil, err
	}

	return &HandoffArchive{Path: "checkpoint.tar.gz", SHA256: hex.EncodeToString(digest.Sum(nil)), Bytes: info.Size()}, nil
}

func validateHandoffArchiveSize(info os.FileInfo, options CheckpointArchiveOptions) error {
	if !info.Mode().IsRegular() {
		return errors.New("handoff archive must be a regular file")
	}
	bytes, files := options.MaxBytes, options.MaxFiles
	if bytes == 0 {
		bytes = defaultCheckpointArchiveBytes
	}
	if files == 0 {
		files = defaultCheckpointArchiveFiles
	}
	const maximum = int64(^uint64(0) >> 1)
	overhead := int64(maxCheckpointManifestSize + maxCheckpointArchiveTrailer)
	if files > (maximum-overhead)/2048 {
		return errors.New("handoff file limit exceeds supported bounds")
	}
	overhead += files * 2048
	if bytes > (maximum-overhead)/2 {
		return errors.New("handoff byte limit exceeds supported bounds")
	}
	if info.Size() > bytes*2+overhead {
		return errors.New("compressed handoff archive exceeds configured bounds")
	}
	return nil
}

func openHandoffFile(path string) (*os.File, error) {
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	root, err := rooted.Open(parent)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenRegularFile(filepath.Base(path))
}
