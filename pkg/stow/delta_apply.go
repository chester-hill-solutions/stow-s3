package stow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ApplyDelta brings a checkpoint to the state a delta describes, and returns the
// new checkpoint. The original is never modified, so a refused application leaves
// the target exactly as it was.
//
// Every change is checked before any of them is written. That ordering is the
// point: a delta applied half way is a target that matches neither end, which is
// worse than a refusal because it is silent. So the preconditions are collected
// and verified first, and only a delta that applies cleanly is written.
func ApplyDelta(ctx context.Context, registryDir, baseID string, delta *DeltaDocument) (CheckpointInfo, error) {
	return ApplyDeltaWithOptions(ctx, registryDir, baseID, delta, DeltaOptions{})
}

// ApplyDeltaWithOptions applies a transported delta when the receiver explicitly
// consents to carrying sensitive-looking paths as well as the sender's consent.
func ApplyDeltaWithOptions(ctx context.Context, registryDir, baseID string, delta *DeltaDocument, options DeltaOptions) (CheckpointInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateDeltaDocument(delta, options.IncludeSensitive); err != nil {
		return CheckpointInfo{}, err
	}
	if err := ctx.Err(); err != nil {
		return CheckpointInfo{}, err
	}
	if baseID == "" {
		return CheckpointInfo{}, errors.New("stow: a delta needs a base checkpoint to apply to")
	}

	base, err := LoadCheckpoint(registryDir, baseID)
	if err != nil {
		return CheckpointInfo{}, err
	}
	baseDir, err := checkpointDirectory(registryDir, baseID)
	if err != nil {
		return CheckpointInfo{}, err
	}
	current := indexCheckpointFiles(base)

	// Verify every precondition before writing anything.
	for _, change := range delta.Changes {
		if err := checkDeltaPrecondition(change, current); err != nil {
			return CheckpointInfo{}, err
		}
	}

	registryDir = filepath.Dir(filepath.Dir(baseDir))
	_, stage, err := stageCheckpointDirectory(registryDir)
	if err != nil {
		return CheckpointInfo{}, fmt.Errorf("stow: stage delta: %w", err)
	}
	defer os.RemoveAll(stage)

	files := filepath.Join(stage, "files")
	if err := os.MkdirAll(files, 0o700); err != nil {
		return CheckpointInfo{}, fmt.Errorf("stow: stage delta files: %w", err)
	}

	// Start from the base, then replay the changes into the staged copy.
	if err := stageBaseFiles(baseDir, files, current); err != nil {
		return CheckpointInfo{}, err
	}
	if err := replayDeltaChanges(delta, files); err != nil {
		return CheckpointInfo{}, err
	}

	return publishDeltaCheckpoint(ctx, registryDir, stage, base)
}

// stageBaseFiles copies the base's files into the staging tree, so a replay only
// has to express what differs.
func stageBaseFiles(baseDir, files string, current map[string]CheckpointFile) error {
	for path, file := range current {
		if err := copyCheckpointFile(baseDir, files, path, file); err != nil {
			return err
		}
	}
	return nil
}

// replayDeltaChanges writes the delta's additions and changes over the staged base
// and applies its deletions. Each kind is handled by its own small function so
// that an unknown kind is a refusal rather than a silently skipped path.
func replayDeltaChanges(delta *DeltaDocument, files string) error {
	for _, change := range delta.Changes {
		var err error
		switch change.Kind {
		case DeltaChangeDeleted:
			err = applyDeltaDelete(files, change)
		case DeltaChangeAdded, DeltaChangeChanged:
			err = applyDeltaWrite(delta, files, change)
		default:
			err = fmt.Errorf("stow: delta change %q has unknown kind %q", change.Path, change.Kind)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func applyDeltaDelete(files string, change DeltaChange) error {
	err := os.Remove(filepath.Join(files, filepath.FromSlash(change.Path)))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stow: apply delta delete %q: %w", change.Path, err)
	}
	return nil
}

func applyDeltaWrite(delta *DeltaDocument, files string, change DeltaChange) error {
	data, ok := delta.Content[change.Path]
	if !ok {
		return fmt.Errorf("stow: delta carries no content for %q", change.Path)
	}
	// The content is verified against the digest the document itself declares
	// before a byte is written. Create verifies what it reads out of a
	// checkpoint, but a document that came through a file carries whatever its
	// author put there, and a delta that writes unverified bytes is a delta that
	// can change a working set without changing what it claims to change.
	if err := verifyDeltaContent(change, data); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if change.To != nil && change.To.Mode != 0 {
		mode = os.FileMode(change.To.Mode) & os.ModePerm
	}
	destination := filepath.Join(files, filepath.FromSlash(change.Path))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("stow: apply delta %q: %w", change.Path, err)
	}
	if err := os.WriteFile(destination, data, mode); err != nil {
		return fmt.Errorf("stow: apply delta %q: %w", change.Path, err)
	}
	return os.Chmod(destination, mode)
}

// verifyDeltaContent checks the bytes against the size and digest the change
// declares. A change that declares no digest cannot be verified, and a delta
// writer that could omit one would make every later check optional, so the
// omission is refused rather than tolerated.
func verifyDeltaContent(change DeltaChange, data []byte) error {
	if change.To == nil || change.To.SHA256 == "" {
		return fmt.Errorf("stow: delta change %q carries content but declares no digest", change.Path)
	}
	if int64(len(data)) != change.To.Size {
		return fmt.Errorf("stow: delta content %q is %d bytes, its change says %d",
			change.Path, len(data), change.To.Size)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != change.To.SHA256 {
		return fmt.Errorf("stow: delta content %q failed integrity validation", change.Path)
	}
	return nil
}

// checkDeltaPrecondition is the conflict rule, one path at a time.
//
// An added path asserts absence, which is the same shape as an If-None-Match of
// "*" in run-through: the delta claims the path is new, so a target that already
// has it belongs to someone else and taking it would lose their work.
func checkDeltaPrecondition(change DeltaChange, current map[string]CheckpointFile) error {
	existing, present := current[change.Path]
	switch change.Kind {
	case DeltaChangeAdded:
		if present {
			return fmt.Errorf("%w: %q is marked added but the target already has it", ErrDeltaConflict, change.Path)
		}
	case DeltaChangeDeleted:
		if !present {
			return fmt.Errorf("%w: %q is marked deleted but the target does not have it", ErrDeltaConflict, change.Path)
		}
		if change.From != nil && existing != *change.From {
			return fmt.Errorf("%w: %q is marked deleted from %s but the target holds %s",
				ErrDeltaConflict, change.Path, shortDigest(change.From.SHA256), shortDigest(existing.SHA256))
		}
	case DeltaChangeChanged:
		if !present {
			return fmt.Errorf("%w: %q is marked changed but the target does not have it", ErrDeltaConflict, change.Path)
		}
		if change.From != nil && existing != *change.From {
			return fmt.Errorf("%w: %q is marked changed from %s but the target holds %s",
				ErrDeltaConflict, change.Path, shortDigest(change.From.SHA256), shortDigest(existing.SHA256))
		}
	default:
		return fmt.Errorf("stow: delta change %q has unknown kind %q", change.Path, change.Kind)
	}
	return nil
}

func copyCheckpointFile(fromDir, toDir, path string, file CheckpointFile) error {
	destination := filepath.Join(toDir, filepath.FromSlash(path))
	data, err := readVerifiedCheckpointFile(fromDir, file)
	if err != nil {
		return fmt.Errorf("stow: read base file %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("stow: stage base file %q: %w", path, err)
	}
	mode := os.FileMode(0o644)
	if file.Mode != 0 {
		mode = os.FileMode(file.Mode) & os.ModePerm
	}
	return os.WriteFile(destination, data, mode)
}

func shortDigest(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	if digest == "" {
		return "(none)"
	}
	return digest
}

// publishDeltaCheckpoint turns a staged tree into a checkpoint of its own.
//
// It publishes by rename, so a reader never sees a partial checkpoint, and it
// refuses an ID that already exists rather than replacing one — the same rule the
// import path follows. A delta applied twice must produce two checkpoints, not
// overwrite the first.
func publishDeltaCheckpoint(ctx context.Context, registryDir, stage string, base CheckpointManifest) (CheckpointInfo, error) {
	checkpointRoot := filepath.Join(registryDir, "checkpoints")
	if err := os.MkdirAll(checkpointRoot, 0o700); err != nil {
		return CheckpointInfo{}, fmt.Errorf("stow: create checkpoint store: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return CheckpointInfo{}, err
	}

	// The published layout is <checkpoint>/manifest.json plus <checkpoint>/files/<path>,
	// so the scan root is the staged files directory. Scanning the stage itself
	// would record every path with a "files/" prefix, and the manifest would then
	// disagree with the layout the loader expects.
	files, excluded, totalBytes, err := scanCheckpointFiles(filepath.Join(stage, "files"), true)
	if err != nil {
		return CheckpointInfo{}, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	id, err := newCheckpointID()
	if err != nil {
		return CheckpointInfo{}, err
	}
	target := checkpointRoot + string(filepath.Separator) + id
	if _, err := os.Stat(target); err == nil {
		return CheckpointInfo{}, fmt.Errorf("stow: checkpoint %q already exists", id)
	}

	manifest := CheckpointManifest{
		Version:     base.Version,
		ID:          id,
		WorkspaceID: base.WorkspaceID,
		ParentID:    base.ID,
		Created:     time.Now().UTC(),
		Files:       files,
		Excluded:    excluded,
	}
	if err := publishCheckpoint(stage, checkpointRoot, id, manifest); err != nil {
		return CheckpointInfo{}, err
	}
	return CheckpointInfo{
		ID:          manifest.ID,
		WorkspaceID: manifest.WorkspaceID,
		ParentID:    manifest.ParentID,
		Created:     manifest.Created,
		Files:       int64(len(files)),
		Bytes:       totalBytes,
		Excluded:    excluded,
	}, nil
}

// ReadCheckpointFile reads one file's bytes out of a published checkpoint,
// verifying the digest the manifest recorded. A caller checking what a delta
// actually produced should not have to reach into the registry layout to do it.
func ReadCheckpointFile(registryDir, checkpointID, path string) ([]byte, error) {
	manifest, err := LoadCheckpoint(registryDir, checkpointID)
	if err != nil {
		return nil, err
	}
	var expected *CheckpointFile
	for i := range manifest.Files {
		if manifest.Files[i].Path == path {
			expected = &manifest.Files[i]
			break
		}
	}
	if expected == nil {
		return nil, fmt.Errorf("stow: checkpoint %s has no %q", checkpointID, path)
	}
	dir, err := checkpointDirectory(registryDir, checkpointID)
	if err != nil {
		return nil, err
	}
	return readVerifiedCheckpointFile(dir, *expected)
}
