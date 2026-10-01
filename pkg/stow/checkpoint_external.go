package stow

import (
	"context"
	"fmt"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// WorkspaceReference is what the registry records about a workspace, read without
// opening it. Answering "is this registered, where does it live, and what are its
// retention caps" by opening the workspace would claim the session, which is the one
// thing a caller that is only asking cannot do to somebody else's live work.
type WorkspaceReference struct {
	WorkingDirectory string
	ID               string
	Dir              string
	Bucket           string
	// The caps are recorded rather than derived, so a capture from outside the
	// workspace enforces the same limits its handle would.
	MaxCheckpointBytes int64
	MaxCheckpoints     int64
}

// LookupWorkspace reads one workspace's registry entry: no process, no claims, no writes.
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
// using right now: the operation an orchestrator needs and could not perform, since
// the answer to "what has it done so far?" must not require the agent to stop. It
// does not claim the session and writes only into the checkpoint store. A tree a live
// agent is writing may change mid-capture, and then this refuses rather than
// publishing a snapshot of two different moments.
//
// It consults no policy. CheckpointOfAuthorized is the same capture with a principal
// named; this entry point passes authority.All() and no policy, which is what
// preserves its behaviour, and callers relying on that should say so at the call
// site rather than assume a gate is here.
func CheckpointOf(ctx context.Context, registryDir, id string, options CheckpointOptions) (CheckpointInfo, error) {
	return CheckpointOfAuthorized(ctx, registryDir, id, options, CapturePrincipal{Environment: authority.All()})
}

// CapturePrincipal is what a capture from outside the workspace is decided by. It
// exists because that path opens no runtime, so there is no authority to read off one
// and naming the two principals as parameters is what keeps the caller from inventing
// a third. A nil Policy means no policy, as with WorkspaceOptions.Policy.
type CapturePrincipal struct {
	Environment authority.Authority
	Policy      policy.Source
}

// CheckpointOfAuthorized is CheckpointOf with its principal named. Asked before the
// capture lock, so a refusal costs nothing and holds nothing.
func CheckpointOfAuthorized(ctx context.Context, registryDir, id string, options CheckpointOptions, principal CapturePrincipal) (CheckpointInfo, error) {
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
	if principal.Policy != nil {
		set, err := principal.Policy()
		if err != nil {
			return CheckpointInfo{}, err
		}
		if _, err := set.Authorize(principal.Environment, policy.Workspace(reference.ID, ""), authority.WorkspaceCapture); err != nil {
			return CheckpointInfo{}, err
		}
	} else if err := principal.Environment.Check(authority.WorkspaceCapture); err != nil {
		return CheckpointInfo{}, err
	}
	// The capture lock is what makes the retention caps exact when two captures of one
	// workspace are in flight: the session says "somebody is using this", this says
	// "do not publish two checkpoints past the same cap".
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
