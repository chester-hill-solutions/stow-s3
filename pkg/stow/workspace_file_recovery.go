package stow

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type recoveryBootstrap struct {
	registryDir                        string
	registry                           *workspace.Registry
	entry                              workspace.Entry
	policy                             Authority
	maxBytes, maxObjects, maxMultipart int64
	maxCheckpointBytes, maxCheckpoints int64
	gate                               *workspace.MutationLock
}

func resumeWorkspaceForFileRecovery(options WorkspaceOptions, id string) (*Workspace, error) {
	bootstrap, err := prepareRecoveryBootstrap(options, id)
	if err != nil {
		return nil, err
	}
	defer bootstrap.gate.Release()
	session, err := acquireRecoverySession(bootstrap.entry)
	if err != nil {
		return nil, err
	}
	claimed := true
	defer func() {
		if claimed {
			_ = session.Release()
		}
	}()
	store, err := recoveryStore(bootstrap.entry, options.Now)
	if err != nil {
		return nil, err
	}
	if store.IsOwned() != bootstrap.entry.Owned {
		_ = store.Close()
		return nil, storage.ErrWorkspaceFileBlocked
	}
	instance, err := runtime.OpenWithStoreForWorkspaceFileReceiptResolution(runtime.Options{Backend: runtime.BackendWorkspace,
		MaxBytes: bootstrap.maxBytes, MaxObjects: bootstrap.maxObjects, MaxMultipartUploads: bootstrap.maxMultipart, Authority: &bootstrap.policy}, store)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	workDir, err := registeredWorkingDirectory(bootstrap.entry)
	if err != nil {
		_ = instance.Close()
		return nil, err
	}
	ws := &Workspace{Runtime: embedded.WrapInstance(instance), dir: store.Root(), workingDirectory: workDir,
		bucket: store.WorkspaceBucket(), id: store.ID(), store: store, instance: instance, session: session,
		registry: bootstrap.registry, registryDir: bootstrap.registryDir, maxCheckpointBytes: bootstrap.maxCheckpointBytes,
		maxCheckpoints: bootstrap.maxCheckpoints, now: options.Now}
	claimed = false
	return ws, nil
}

func prepareRecoveryBootstrap(options WorkspaceOptions, id string) (recoveryBootstrap, error) {
	if options.Policy != nil {
		return recoveryBootstrap{}, storage.ErrWorkspaceFileUnsupported
	}
	registryDir, err := ResolveRegistryDir(options.RegistryDir, options.Team)
	if err != nil {
		return recoveryBootstrap{}, err
	}
	gate, err := workspace.AcquireMutationCapture(registryDir, id)
	if err != nil {
		return recoveryBootstrap{}, err
	}
	bootstrap, err := lookupRecoveryWorkspace(registryDir, id)
	if err != nil {
		_ = gate.Release()
		return recoveryBootstrap{}, err
	}
	bootstrap.gate, bootstrap.registryDir = gate, registryDir
	resolved := options
	resolved.RegistryDir, resolved.Team = registryDir, ""
	bootstrap.policy, bootstrap.maxBytes, bootstrap.maxObjects, _, bootstrap.maxCheckpointBytes, bootstrap.maxCheckpoints, err = resumedPolicy(bootstrap.entry, resolved)
	if err == nil {
		bootstrap.maxMultipart, err = resumeMultipartLimit(bootstrap.entry.MaxMultipartUploads, resolved.MaxMultipartUploads)
	}
	if err == nil {
		err = bootstrap.policy.Check(ObjectRead)
	}
	if err == nil {
		err = workspace.ValidateRegisteredSessionRoot(bootstrap.entry.Dir)
	}
	if err != nil {
		_ = gate.Release()
		return recoveryBootstrap{}, err
	}
	return bootstrap, nil
}

func lookupRecoveryWorkspace(registryDir, id string) (recoveryBootstrap, error) {
	registry, err := workspace.OpenRegistryReadOnly(registryDir)
	if err != nil {
		return recoveryBootstrap{}, err
	}
	entry, err := lookupWorkspaceEntry(registry, id)
	if err != nil {
		return recoveryBootstrap{}, err
	}
	return recoveryBootstrap{registry: registry, entry: entry}, nil
}

func acquireRecoverySession(entry workspace.Entry) (*workspace.Session, error) {
	if err := workspace.ValidateRegisteredSessionRoot(entry.Dir); err != nil {
		return nil, err
	}
	session, err := workspace.AcquireSession(entry.Dir)
	if err != nil {
		return nil, fmt.Errorf("stow: claim workspace: %w", err)
	}
	return session, nil
}

func recoveryStore(entry workspace.Entry, now func() time.Time) (*workspace.Store, error) {
	if err := workspace.ValidateRegisteredWorkspace(entry.Dir, entry.ID, entry.Bucket, entry.Owned); err != nil {
		return nil, storage.ErrWorkspaceFileBlocked
	}
	store, err := workspace.NewForFileSaveRecovery(workspace.Options{Root: entry.Dir, Bucket: entry.Bucket, Now: now}, entry.ID)
	if err != nil {
		return nil, fmt.Errorf("stow: open workspace file recovery: %w", err)
	}
	return store, nil
}

func registeredWorkingDirectory(entry workspace.Entry) (string, error) {
	working := entry.WorkingDirectory
	if working == "" {
		working = entry.Dir
	}
	rootAbs, err := filepath.Abs(entry.Dir)
	if err != nil {
		return "", err
	}
	workAbs, err := filepath.Abs(working)
	if err != nil || !pathWithin(rootAbs, workAbs) {
		return "", fmt.Errorf("stow: registered working directory is invalid")
	}
	info, err := os.Stat(workAbs)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("stow: registered working directory is unavailable")
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	workReal, err := filepath.EvalSymlinks(workAbs)
	if err != nil || !pathWithin(rootReal, workReal) {
		return "", fmt.Errorf("stow: registered working directory escapes workspace")
	}
	return workAbs, nil
}
