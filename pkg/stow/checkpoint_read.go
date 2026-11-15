package stow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

const (
	checkpointReadDefaultLifetime = 5 * time.Minute
	checkpointReadMaxLifetime     = 5 * time.Minute
	checkpointReadDefaultVerify   = 64 << 20
	checkpointReadMaxVerify       = 64 << 20
	checkpointReadMaxFileBytes    = 64 << 10
	checkpointReadManifestBytes   = 4 << 20
	checkpointReadMaxFiles        = 10_000
	checkpointReadMaxMetadata     = 4 << 20
)

var (
	ErrCheckpointInUse           = workspace.ErrCheckpointInUse
	ErrCheckpointReadUnavailable = errors.New("stow: checkpoint resource is unavailable")
	ErrCheckpointReadTooLarge    = errors.New("stow: checkpoint resource is too large")
	ErrCheckpointReadExpired     = errors.New("stow: checkpoint reader expired")
	ErrCheckpointReadClosed      = errors.New("stow: checkpoint reader is closed")
)

type CheckpointReaderOptions struct {
	RegistryDir    string
	WorkspaceID    string
	CheckpointID   string
	ReadRoots      []string
	Lifetime       time.Duration
	MaxVerifyBytes int64
}

type CheckpointReadReference struct {
	WorkspaceID    string
	CheckpointID   string
	Version        int
	ManifestSHA256 string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ReadRoots      []string
}

type CheckpointReadDirectory struct {
	Entries []WorkspaceReadEntry
	Offset  int
	Total   int
}

type CheckpointReadResult struct {
	Path   string
	Data   []byte
	Offset int64
	Size   int64
	SHA256 string
}

type CheckpointReader struct {
	mu          sync.Mutex
	reference   CheckpointReadReference
	root        *rooted.Root
	workspace   *workspace.CaptureLock
	checkpoint  *workspace.CaptureLock
	files       map[string]CheckpointFile
	directories map[string][]WorkspaceReadEntry
	readRoots   []string
	maxVerify   int64
	closed      bool
	timer       *time.Timer
}

type checkpointReaderSetup struct {
	workspace  *workspace.CaptureLock
	checkpoint *workspace.CaptureLock
	root       *rooted.Root
	manifest   CheckpointManifest
	digest     string
}

func OpenCheckpointReader(ctx context.Context, options CheckpointReaderOptions) (*CheckpointReader, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lifetime, maxVerify, err := checkpointReaderBounds(options)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	registryDir, err := ResolveRegistryDir(options.RegistryDir, "")
	if err != nil || !workspace.ValidWorkspaceID(options.WorkspaceID) || !validCheckpointID(options.CheckpointID) {
		return nil, ErrCheckpointReadUnavailable
	}
	expiresAt := time.Now().Add(lifetime)
	setup, err := prepareCheckpointReader(ctx, registryDir, options, expiresAt)
	if err != nil {
		return nil, err
	}
	files, directories, roots, err := indexCheckpointReadFiles(setup.manifest.Files, options.ReadRoots)
	if err != nil {
		setup.release()
		return nil, err
	}
	if ctx.Err() != nil || !time.Now().Before(expiresAt) || setup.root.Check() != nil {
		setup.release()
		return nil, ErrCheckpointReadUnavailable
	}
	reader := &CheckpointReader{
		reference: CheckpointReadReference{WorkspaceID: options.WorkspaceID, CheckpointID: options.CheckpointID,
			Version: setup.manifest.Version, ManifestSHA256: setup.digest, CreatedAt: setup.manifest.Created.UTC(), ExpiresAt: expiresAt,
			ReadRoots: append([]string(nil), roots...)},
		root: setup.root, workspace: setup.workspace, checkpoint: setup.checkpoint,
		files: files, directories: directories, readRoots: roots, maxVerify: maxVerify,
	}
	if !reader.startExpiryTimer() {
		setup.release()
		return nil, ErrCheckpointReadUnavailable
	}
	return reader, nil
}

func prepareCheckpointReader(ctx context.Context, registryDir string, options CheckpointReaderOptions, expiresAt time.Time) (*checkpointReaderSetup, error) {
	workspaceLease, err := workspace.AcquireReadAttachment(registryDir, options.WorkspaceID)
	if err != nil {
		return nil, ErrCheckpointReadUnavailable
	}
	setup := &checkpointReaderSetup{workspace: workspaceLease}
	ready := false
	defer func() {
		if !ready {
			setup.release()
		}
	}()
	entry, err := LookupWorkspace(registryDir, options.WorkspaceID)
	if err != nil || ctx.Err() != nil || !time.Now().Before(expiresAt) {
		return nil, ErrCheckpointReadUnavailable
	}
	setup.checkpoint, err = workspace.AcquireCheckpointReadAttachment(registryDir, options.CheckpointID)
	if err != nil {
		return nil, ErrCheckpointReadUnavailable
	}
	if err := openCheckpointReaderRoot(setup, registryDir, options.CheckpointID); err != nil {
		return nil, ErrCheckpointReadUnavailable
	}
	setup.manifest, setup.digest, err = readCheckpointManifest(ctx, setup.root, options.CheckpointID)
	if err != nil || setup.manifest.WorkspaceID != entry.ID || setup.manifest.WorkspaceID != options.WorkspaceID || !time.Now().Before(expiresAt) {
		return nil, ErrCheckpointReadUnavailable
	}
	ready = true
	return setup, nil
}

func openCheckpointReaderRoot(setup *checkpointReaderSetup, registryDir, checkpointID string) error {
	checkpointDir, err := checkpointDirectory(registryDir, checkpointID)
	if err != nil {
		return err
	}
	setup.root, err = rooted.Open(checkpointDir)
	return err
}

func (s *checkpointReaderSetup) release() {
	if s.root != nil {
		_ = s.root.Close()
	}
	if s.checkpoint != nil {
		_ = s.checkpoint.Release()
	}
	if s.workspace != nil {
		_ = s.workspace.Release()
	}
}

func (r *CheckpointReader) startExpiryTimer() bool {
	delay := time.Until(r.reference.ExpiresAt)
	if delay <= 0 {
		return false
	}
	r.mu.Lock()
	r.timer = time.AfterFunc(delay, func() { _ = r.Close() })
	r.mu.Unlock()
	return true
}

func checkpointReaderBounds(options CheckpointReaderOptions) (time.Duration, int64, error) {
	lifetime := options.Lifetime
	if lifetime == 0 {
		lifetime = checkpointReadDefaultLifetime
	}
	maxVerify := options.MaxVerifyBytes
	if maxVerify == 0 {
		maxVerify = checkpointReadDefaultVerify
	}
	if lifetime <= 0 || lifetime > checkpointReadMaxLifetime || maxVerify <= 0 || maxVerify > checkpointReadMaxVerify {
		return 0, 0, ErrCheckpointReadUnavailable
	}
	return lifetime, maxVerify, nil
}

func readCheckpointManifest(ctx context.Context, root *rooted.Root, id string) (CheckpointManifest, string, error) {
	data, err := readCheckpointManifestBytes(ctx, root)
	if err != nil {
		return CheckpointManifest{}, "", ErrCheckpointReadUnavailable
	}
	manifest, err := decodeCheckpointManifest(id, data)
	if err != nil {
		return CheckpointManifest{}, "", ErrCheckpointReadUnavailable
	}
	return manifest, checkpointManifestDigest(data), nil
}

func readCheckpointManifestBytes(ctx context.Context, root *rooted.Root) ([]byte, error) {
	if err := ctx.Err(); err != nil || root.Check() != nil {
		return nil, ErrCheckpointReadUnavailable
	}
	file, err := root.OpenRegularFile("manifest.json")
	if err != nil {
		return nil, ErrCheckpointReadUnavailable
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 || before.Size() > checkpointReadManifestBytes {
		return nil, ErrCheckpointReadUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, checkpointReadManifestBytes+1))
	if err != nil || len(data) > checkpointReadManifestBytes || ctx.Err() != nil {
		return nil, ErrCheckpointReadUnavailable
	}
	after, err := file.Stat()
	if err != nil || !sameCheckpointFile(before, after) || int64(len(data)) != after.Size() || root.Check() != nil {
		return nil, ErrCheckpointReadUnavailable
	}
	return data, nil
}

func sameCheckpointFile(before, after os.FileInfo) bool {
	return os.SameFile(before, after) && before.Size() == after.Size() &&
		before.Mode() == after.Mode() && before.ModTime().Equal(after.ModTime())
}

func (r *CheckpointReader) Reference() CheckpointReadReference {
	r.mu.Lock()
	defer r.mu.Unlock()
	reference := r.reference
	reference.ReadRoots = append([]string(nil), r.reference.ReadRoots...)
	return reference
}

func (r *CheckpointReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.timer != nil {
		r.timer.Stop()
	}
	rootErr := r.root.Close()
	checkpointErr := r.checkpoint.Release()
	workspaceErr := r.workspace.Release()
	return errors.Join(rootErr, checkpointErr, workspaceErr)
}

func (r *CheckpointReader) operationContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if !time.Now().Before(r.reference.ExpiresAt) {
		return nil, nil, ErrCheckpointReadExpired
	}
	if r.closed {
		return nil, nil, ErrCheckpointReadClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opCtx, cancel := context.WithDeadline(ctx, r.reference.ExpiresAt)
	return opCtx, cancel, nil
}

func (r *CheckpointReader) operationError(ctx context.Context, err error) error {
	if err != nil && ctx != nil && ctx.Err() != nil && time.Now().Before(r.reference.ExpiresAt) {
		return ctx.Err()
	}
	if !time.Now().Before(r.reference.ExpiresAt) {
		return ErrCheckpointReadExpired
	}
	if err == nil {
		return nil
	}
	return err
}

func checkpointManifestDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
