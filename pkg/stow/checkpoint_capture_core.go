package stow

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
)

type captureTarget struct {
	receipt     *checkpointReceipt
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

func captureCheckpoint(ctx context.Context, target captureTarget, options CheckpointOptions) (CheckpointInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return CheckpointInfo{}, err
	}
	if err := target.validateParent(options.ParentID); err != nil {
		return CheckpointInfo{}, err
	}
	source, err := rooted.Open(target.dir)
	if err != nil {
		return CheckpointInfo{}, err
	}
	defer source.Close()
	before, excluded, size, err := checkpointInputs(ctx, source, options)
	if err != nil {
		return CheckpointInfo{}, err
	}
	portable, err := preparePortableCapture(ctx, target, options, before)
	if err != nil {
		return CheckpointInfo{}, err
	}
	size, err = portableCaptureSize(before, portable, options)
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
	if err := copyCheckpointFiles(ctx, source, tempDir, before); err != nil {
		return CheckpointInfo{}, err
	}
	manifest, err := target.stageManifest(tempDir, before, excluded, options)
	if err != nil {
		return CheckpointInfo{}, err
	}
	if err := finishPortableCapture(ctx, target, tempDir, portable, &manifest); err != nil {
		return CheckpointInfo{}, err
	}
	if err := verifyCheckpointCapture(ctx, source, before, excluded, options.IncludeSensitiveFiles); err != nil {
		return CheckpointInfo{}, err
	}
	if err := publishCheckpointContext(ctx, tempDir, checkpointRoot, manifest.ID, manifest); err != nil {
		return CheckpointInfo{}, err
	}
	return CheckpointInfo{
		Version: manifest.Version, Objects: int64(len(manifest.Objects)),
		ID: manifest.ID, WorkspaceID: target.workspaceID, ParentID: options.ParentID,
		Created: manifest.Created, Files: int64(len(before)), Bytes: size, Excluded: manifest.Excluded,
	}, nil
}

// checkRetention runs under the workspace capture gate.
func (t captureTarget) checkRetention(nextBytes int64) error {
	if err := checkRegistryCheckpointAdmission(t.registryDir, t.workspaceID, nextBytes); err != nil {
		return err
	}
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
		return checkpointFailure("capacity_exceeded", "admit", "not_committed", fmt.Errorf("checkpoint count limit reached (%d of %d); remove a checkpoint or raise the workspace limit", count, maxCount))
	}
	if maxBytes > 0 && (total > maxBytes || nextBytes > maxBytes-total) {
		return checkpointFailure("capacity_exceeded", "admit", "not_committed", fmt.Errorf("checkpoint byte limit exceeded (%d existing + %d new > %d); remove a checkpoint or raise the workspace limit", total, nextBytes, maxBytes))
	}
	return nil
}

func (t captureTarget) stageManifest(stage string, files []CheckpointFile, excluded []string, options CheckpointOptions) (CheckpointManifest, error) {
	id, err := newCheckpointID()
	if err != nil {
		return CheckpointManifest{}, err
	}
	if t.receipt != nil {
		id = t.receipt.CheckpointID
	}
	if err := writeCheckpointReceipt(stage, t.receipt); err != nil {
		return CheckpointManifest{}, err
	}
	return CheckpointManifest{Version: checkpointVersion, ID: id, WorkspaceID: t.workspaceID,
		ParentID: options.ParentID, Created: t.clock()().UTC(), Files: files, Excluded: excluded}, nil
}

func (t captureTarget) validateParent(parentID string) error {
	if parentID == "" {
		return nil
	}
	if err := validateCheckpointParent(t.registryDir, t.workspaceID, parentID); err != nil {
		return checkpointFailure("invalid_parent", "validate", "not_committed", err)
	}
	return nil
}
