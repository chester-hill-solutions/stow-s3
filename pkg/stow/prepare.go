package stow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceInput copies one local file or directory into a prepared workspace.
// Destination is always relative to the workspace root. Directory inputs copy
// their contents beneath Destination.
type WorkspaceInput struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// GitRepositoryInput fetches one local repository commit into a fresh shallow
// repository and checks it out detached. It never registers a worktree in or
// modifies the source repository.
type GitRepositoryInput struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Ref         string `json:"ref"`
}

// PreparedRepository records the immutable revision selected for one input.
type PreparedRepository struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Ref         string `json:"ref"`
	Commit      string `json:"commit"`
}

// PrepareOptions describes a local, deterministic workspace seed. Git inputs
// fetch one selected commit without copying prior history. Preparation does not
// read credential stores or modify the source inputs.
type PrepareOptions struct {
	WorkspaceOptions
	WorkingDirectory       string               `json:"working_directory,omitempty"`
	Inputs                 []WorkspaceInput     `json:"inputs,omitempty"`
	Repositories           []GitRepositoryInput `json:"repositories,omitempty"`
	IncludeSensitiveInputs bool                 `json:"include_sensitive_inputs,omitempty"`
	// MaxWorkspaces narrows the standing registry cap for this prepare; zero adds no call-specific cap.
	MaxWorkspaces int64 `json:"max_workspaces,omitempty"`
}

// WorkspaceTaskManifest is the versioned JSON input accepted by workspace
// preparation tools. The path naming an input is local to the preparing host.
type WorkspaceTaskManifest struct {
	Version            int                  `json:"version"`
	Root               string               `json:"root"`
	WorkingDirectory   string               `json:"working_directory,omitempty"`
	Inputs             []WorkspaceInput     `json:"inputs,omitempty"`
	Repositories       []GitRepositoryInput `json:"repositories,omitempty"`
	MaxBytes           int64                `json:"max_bytes,omitempty"`
	MaxObjects         int64                `json:"max_objects,omitempty"`
	MaxCheckpointBytes int64                `json:"max_checkpoint_bytes,omitempty"`
	MaxCheckpoints     int64                `json:"max_checkpoints,omitempty"`
	// MaxWorkspaces narrows the standing registry cap; zero adds no call-specific cap.
	MaxWorkspaces int64 `json:"max_workspaces,omitempty"`
	TTLSeconds    int64 `json:"ttl_seconds,omitempty"`
	// Team files the workspace under one team's partition of the registry. On a
	// shared runner this is what keeps one job's workspaces, checkpoints, and
	// sweeps away from another's, and it is recorded in the manifest so the
	// partitioning is declared where the workspace is asked for rather than
	// inferred by whoever resumes it.
	Team                   string `json:"team,omitempty"`
	RegistryDir            string `json:"registry_dir,omitempty"`
	Bucket                 string `json:"bucket,omitempty"`
	IncludeSensitiveInputs bool   `json:"include_sensitive_inputs,omitempty"`
}

// PreparedWorkspace is the ready-to-launch result. WorkingDirectory is the
// directory an external agent should use as its cwd.
type PreparedWorkspace struct {
	Workspace        *Workspace
	WorkingDirectory string
	SeededBytes      int64
	SeededObjects    int64
	BaseIdentity     string
	Repositories     []PreparedRepository
	// RegistryEntries is how many workspaces the registry held when this one was
	// added, so a caller can see its headroom instead of discovering the bound by
	// being refused. It is counted only when a bound is declared; a prepare with no
	// bound reports zero rather than paying for a read nothing will use.
	RegistryEntries int64
	// MaxWorkspaces is the bound that was applied, echoed so a caller reading only
	// the result knows which policy produced the count above.
	MaxWorkspaces int64
}

// discardOnFailure removes a workspace this call created but did not finish, and
// folds any cleanup failure into the error that caused it.
//
// Both failures are reported together rather than one overwriting the other, because
// a caller seeing only "cleanup failed" cannot tell whether the prepare failed for
// its own reason or succeeded and was then torn down. A committed workspace is left
// alone, which is the whole reason the flag is a pointer: the defer closes over it
// and sees the final value.
func discardOnFailure(resultErr error, ws *Workspace, committed *bool) error {
	if *committed || resultErr == nil {
		return resultErr
	}
	closeErr := ws.Close()
	destroyErr := ws.discardUncommitted()
	if closeErr != nil || destroyErr != nil {
		return fmt.Errorf("%w (cleanup close: %v; cleanup destroy: %v)", resultErr, closeErr, destroyErr)
	}
	return resultErr
}

// PrepareWorkspace creates a Stow-owned workspace, stages declared local files
// and selected Git refs, checks the seed against effective runtime limits, then
// returns the directory an agent can use. A failed prepare removes only the
// root this call created.
func PrepareWorkspace(options PrepareOptions) (_ *PreparedWorkspace, resultErr error) {
	if options.Dir == "" {
		return nil, fmt.Errorf("stow: prepare requires a workspace Dir")
	}
	if len(options.Inputs) == 0 && len(options.Repositories) == 0 {
		return nil, fmt.Errorf("stow: prepare requires at least one input")
	}
	root, err := validatePrepareRoot(options.Dir)
	if err != nil {
		return nil, err
	}
	// Before anything is created, so a refusal costs a command rather than a
	// half-made workspace and a cleanup.
	entries, err := checkWorkspaceRetention(options.RegistryDir, options.Team, options.MaxWorkspaces)
	if err != nil {
		return nil, err
	}
	if options.Authority == nil {
		localAuthority := ReadWrite()
		options.Authority = &localAuthority
	}
	options.WorkspaceOptions.maxRegistryWorkspaces = options.MaxWorkspaces
	ws, err := openPreparedWorkspace(options.WorkspaceOptions, root)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() { resultErr = discardOnFailure(resultErr, ws, &committed) }()

	bytes, objects, repositories, err := seedPreparedWorkspace(root, options)
	if err != nil {
		return nil, err
	}
	workingDirectory, err := prepareWorkingDirectory(root, options.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	ws.workingDirectory = workingDirectory
	if err := persistPreparedWorkingDirectory(ws); err != nil {
		return nil, err
	}
	if err := checkSeedLimits(ws, bytes, objects); err != nil {
		return nil, err
	}
	if err := ws.Runtime.inner.RefreshUsage(context.Background()); err != nil {
		return nil, err
	}
	if err := persistPreparedProvenance(root, repositories); err != nil {
		return nil, err
	}
	baseIdentity, err := workspaceFingerprint(root)
	if err != nil {
		return nil, fmt.Errorf("stow: fingerprint prepared inputs: %w", err)
	}
	committed = true
	return &PreparedWorkspace{
		Workspace: ws, WorkingDirectory: workingDirectory,
		SeededBytes: bytes, SeededObjects: objects, BaseIdentity: baseIdentity,
		Repositories:    repositories,
		RegistryEntries: entries + 1, MaxWorkspaces: options.MaxWorkspaces,
	}, nil
}

func seedPreparedWorkspace(root string, options PrepareOptions) (int64, int64, []PreparedRepository, error) {
	bytes, objects, repositories, err := cloneGitRepositories(root, options.Repositories, options.IncludeSensitiveInputs)
	if err != nil {
		return 0, 0, nil, err
	}
	inputBytes, inputObjects, err := seedWorkspaceInputs(root, options.Inputs, options.IncludeSensitiveInputs)
	if err != nil {
		return 0, 0, nil, err
	}
	maxInt64 := int64(^uint64(0) >> 1)
	if inputBytes > maxInt64-bytes || inputObjects > maxInt64-objects {
		return 0, 0, nil, fmt.Errorf("stow: seeded totals exceed supported limits")
	}
	return bytes + inputBytes, objects + inputObjects, repositories, nil
}

func openPreparedWorkspace(options WorkspaceOptions, root string) (*Workspace, error) {
	if err := os.Mkdir(root, 0o755); err != nil {
		return nil, fmt.Errorf("stow: reserve prepare destination: %w", err)
	}
	options.Dir = root
	options.owned = !options.Adopted
	ws, err := OpenWorkspace(options)
	if err == nil {
		return ws, nil
	}
	if cleanupErr := os.RemoveAll(root); cleanupErr != nil {
		return nil, fmt.Errorf("%w (cleanup partial workspace: %v)", err, cleanupErr)
	}
	return nil, err
}

func validatePrepareRoot(dir string) (string, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("stow: resolve workspace Dir: %w", err)
	}
	if _, err := os.Lstat(root); err == nil {
		return "", fmt.Errorf("stow: prepare destination already exists: %s", root)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stow: inspect prepare destination: %w", err)
	}
	parentInfo, err := os.Stat(filepath.Dir(root))
	if err != nil || !parentInfo.IsDir() {
		return "", fmt.Errorf("stow: prepare destination parent must be an existing directory")
	}
	return root, nil
}

func seedWorkspaceInputs(root string, inputs []WorkspaceInput, includeSensitive bool) (int64, int64, error) {
	totals := &seedContext{includeSensitive: includeSensitive}
	for _, input := range inputs {
		if strings.TrimSpace(input.Source) == "" {
			return 0, 0, fmt.Errorf("stow: input source is required")
		}
		destination, err := safeDestination(root, input.Destination)
		if err != nil {
			return 0, 0, fmt.Errorf("stow: invalid input destination %q: %w", input.Destination, err)
		}
		if err := copySeed(input.Source, destination, totals); err != nil {
			return 0, 0, fmt.Errorf("stow: seed %q: %w", input.Source, err)
		}
	}
	return totals.bytes, totals.objects, nil
}

func prepareWorkingDirectory(root, relative string) (string, error) {
	if relative == "" {
		relative = "."
	}
	workingDirectory, err := safeDestination(root, relative)
	if err != nil {
		return "", fmt.Errorf("stow: invalid working directory: %w", err)
	}
	if err := os.MkdirAll(workingDirectory, 0o755); err != nil {
		return "", fmt.Errorf("stow: create working directory: %w", err)
	}
	info, err := os.Stat(workingDirectory)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("stow: working directory is not a directory: %s", workingDirectory)
	}
	return workingDirectory, nil
}

func persistPreparedWorkingDirectory(ws *Workspace) error {
	if ws.registry == nil {
		return nil
	}
	entry, err := lookupWorkspaceEntry(ws.registry, ws.id)
	if err != nil {
		return fmt.Errorf("stow: update prepared workspace registry entry: %w", err)
	}
	entry.WorkingDirectory = ws.workingDirectory
	if err := ws.registry.Register(entry); err != nil {
		return fmt.Errorf("stow: update prepared workspace registry entry: %w", err)
	}
	return nil
}

func checkSeedLimits(ws *Workspace, bytes, objects int64) error {
	limits := ws.Capabilities()
	if limits.MaxBytes > 0 && bytes > limits.MaxBytes {
		return fmt.Errorf("stow: seeded bytes %d exceed workspace limit %d", bytes, limits.MaxBytes)
	}
	if limits.MaxObjects > 0 && objects > limits.MaxObjects {
		return fmt.Errorf("stow: seeded objects %d exceed workspace limit %d", objects, limits.MaxObjects)
	}
	return nil
}

func workspaceFingerprint(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == filepath.Join(root, ".stow") {
			return filepath.SkipDir
		}
		if path != root && strings.EqualFold(entry.Name(), ".git") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected non-regular file in prepared workspace: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(hash, filepath.ToSlash(rel)+"\x00"); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		_, err = io.WriteString(hash, "\x00")
		return err
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}
