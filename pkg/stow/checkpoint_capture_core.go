package stow

import (
	"context"
	"fmt"
	"os"
	"time"
)

// captureTarget is the workspace a capture reads, resolved already.
//
// It is a value rather than a *Workspace because a capture does not need a handle
// and must not require one. Everything a capture touches is the directory (read)
// and the checkpoint store (written); the session lock is none of those, and
// holding it is what made a live workspace impossible to snapshot.
type captureTarget struct {
	dir         string
	registryDir string
	workspaceID string
	// maxCheckpointBytes and maxCheckpoints are the workspace's retention caps,
	// carried here because they are recorded in the registry rather than derived
	// from anything about the directory.
	maxCheckpointBytes int64
	maxCheckpoints     int64
	now                func() time.Time
}

func (t captureTarget) clock() func() time.Time {
	if t.now != nil {
		return t.now
	}
	return time.Now
}

// captureCheckpoint is the whole of a checkpoint: scan, copy, verify, publish.
//
// Both entry points run exactly this, so a snapshot taken from outside a live
// session and one taken from the handle that owns the session are the same
// operation with the same refusals. The only difference between them is who
// resolved the target and who held the capture lock while it ran.
//
// The tree is scanned before the copy and again after it, and a tree that changed
// in between is refused. That is what makes capturing a live workspace safe, and
// it is why no lock is needed to make the bytes consistent: consistency here comes
// from noticing a change, not from preventing one.
func captureCheckpoint(ctx context.Context, target captureTarget, options CheckpointOptions) (CheckpointInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return CheckpointInfo{}, err
	}
	if options.ParentID != "" {
		if err := validateCheckpointParent(target.registryDir, target.workspaceID, options.ParentID); err != nil {
			return CheckpointInfo{}, err
		}
	}
	before, excluded, size, err := checkpointInputs(target.dir, options)
	if err != nil {
		return CheckpointInfo{}, err
	}
	if err := target.checkRetention(size); err != nil {
		return CheckpointInfo{}, err
	}
	checkpointRoot, tempDir, err := stageCheckpointDirectory(target.registryDir)
	if err != nil {
		return CheckpointInfo{}, err
	}
	defer os.RemoveAll(tempDir)
	if err := copyCheckpointFiles(ctx, target.dir, tempDir, before); err != nil {
		return CheckpointInfo{}, err
	}
	if err := verifyCheckpointCapture(target.dir, before, excluded, options.IncludeSensitiveFiles); err != nil {
		return CheckpointInfo{}, err
	}
	id, err := newCheckpointID()
	if err != nil {
		return CheckpointInfo{}, err
	}
	manifest := CheckpointManifest{
		Version: checkpointVersion, ID: id, WorkspaceID: target.workspaceID,
		ParentID: options.ParentID, Created: target.clock()().UTC(),
		Files: before, Excluded: excluded,
	}
	if err := publishCheckpoint(tempDir, checkpointRoot, id, manifest); err != nil {
		return CheckpointInfo{}, err
	}
	return CheckpointInfo{
		ID: id, WorkspaceID: target.workspaceID, ParentID: options.ParentID,
		Created: manifest.Created, Files: int64(len(before)), Bytes: size, Excluded: excluded,
	}, nil
}

// checkRetention accounts only published checkpoints, and the caller holds
// something that excludes a second capture of this workspace: on a handle that is
// checkpointMu, and from outside a session it is the capture lock.
func (t captureTarget) checkRetention(nextBytes int64) error {
	return checkCheckpointRetention(t.registryDir, t.workspaceID, t.maxCheckpointBytes, t.maxCheckpoints, nextBytes)
}

// checkCheckpointRetention is the cap check with its inputs spelled out, so a
// handle and a registry entry can each pass what they know without either of them
// having to be a handle.
func checkCheckpointRetention(registryDir, workspaceID string, maxBytes, maxCount, nextBytes int64) error {
	if maxBytes == 0 && maxCount == 0 {
		return nil
	}
	count, total, err := checkpointRetentionUsage(registryDir, workspaceID)
	if err != nil {
		return err
	}
	if maxCount > 0 && count >= maxCount {
		return fmt.Errorf("stow: checkpoint count limit reached (%d of %d); remove a checkpoint or raise the workspace limit", count, maxCount)
	}
	if maxBytes > 0 && (total > maxBytes || nextBytes > maxBytes-total) {
		return fmt.Errorf("stow: checkpoint byte limit exceeded (%d existing + %d new > %d); remove a checkpoint or raise the workspace limit", total, nextBytes, maxBytes)
	}
	return nil
}
