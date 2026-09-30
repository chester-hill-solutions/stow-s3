package stow

import (
	"context"
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// WorkspaceReference is what the registry records about a workspace, read without
// opening it.
//
// It exists because two questions about a workspace do not need a handle: "is
// this registered, and where does it live?" and "what are its retention caps?".
// Answering either by opening the workspace claims the session, which is the one
// thing a caller that is only asking cannot do to somebody else's live work.
type WorkspaceReference struct {
	WorkingDirectory string
	ID               string
	Dir              string
	Bucket           string
	// MaxCheckpointBytes and MaxCheckpoints are the caps a capture enforces, and
	// they are recorded rather than derived, so a capture from outside the
	// workspace enforces the same limits its handle would.
	MaxCheckpointBytes int64
	MaxCheckpoints     int64
}

// LookupWorkspace reads one workspace's registry entry. It starts no process,
// claims nothing, and writes nothing.
func LookupWorkspace(registryDir, id string) (WorkspaceReference, error) {
	registry, err := openRegistryReadOnly(registryDir, "")
	if err != nil {
		return WorkspaceReference{}, err
	}
	entry, found, err := registry.Lookup(id)
	if err != nil {
		return WorkspaceReference{}, fmt.Errorf("stow: look up workspace %s: %w", id, err)
	}
	if !found {
		return WorkspaceReference{}, fmt.Errorf("stow: no workspace with id %s", id)
	}
	return WorkspaceReference{
		ID: entry.ID, Dir: entry.Dir, Bucket: entry.Bucket, WorkingDirectory: entry.WorkingDirectory,
		MaxCheckpointBytes: entry.MaxCheckpointBytes, MaxCheckpoints: entry.MaxCheckpoints,
	}, nil
}

// CheckpointOf captures a checkpoint of a workspace that another process may be
// using right now.
//
// This is the operation an orchestrator needs and could not perform: an agent holds
// the workspace, and the answer to "what has it done so far?" must not require the
// agent to stop. So this does not claim the session, does not register or touch
// anything, and writes only into the checkpoint store — never into the workspace. The
// capture is the same one Workspace.CreateCheckpoint runs, with the same exclusions,
// the same limits from the registry entry, and the same refusal of a tree that changed
// while it was being read.
//
// A tree a live agent is actively writing may well change mid-capture, and then this
// refuses rather than publishing a snapshot of two different moments. That refusal is
// the correct answer, and it is the one a checkpoint of moving work always has.
func CheckpointOf(ctx context.Context, registryDir, id string, options CheckpointOptions) (CheckpointInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	registryDir, err := ResolveRegistryDir(registryDir, "")
	if err != nil {
		return CheckpointInfo{}, err
	}
	reference, err := LookupWorkspace(registryDir, id)
	if err != nil {
		return CheckpointInfo{}, err
	}
	// The capture lock is what makes the retention caps exact when two captures
	// of one workspace are in flight. It is a different lock from the session for a
	// reason: the session says "somebody is using this", and this one says "do not
	// publish two checkpoints past the same cap".
	lock, err := workspace.AcquireMutationCapture(registryDir, reference.ID)
	if err != nil {
		return CheckpointInfo{}, err
	}
	defer lock.Release()
	reference, err = LookupWorkspace(registryDir, id)
	if err != nil {
		return CheckpointInfo{}, err
	}
	return captureCheckpoint(ctx, captureTarget{
		dir: reference.Dir, registryDir: registryDir, workspaceID: reference.ID,
		maxCheckpointBytes: reference.MaxCheckpointBytes, maxCheckpoints: reference.MaxCheckpoints,
	}, options)
}
