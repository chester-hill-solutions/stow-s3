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
}

// WorkspaceTaskManifest is the versioned JSON input accepted by workspace
// preparation tools. The path naming an input is local to the preparing host.
type WorkspaceTaskManifest struct {
	Version                int                  `json:"version"`
	Root                   string               `json:"root"`
	WorkingDirectory       string               `json:"working_directory,omitempty"`
	Inputs                 []WorkspaceInput     `json:"inputs,omitempty"`
	Repositories           []GitRepositoryInput `json:"repositories,omitempty"`
	MaxBytes               int64                `json:"max_bytes,omitempty"`
	MaxObjects             int64                `json:"max_objects,omitempty"`
	TTLSeconds             int64                `json:"ttl_seconds,omitempty"`
	RegistryDir            string               `json:"registry_dir,omitempty"`
	Bucket                 string               `json:"bucket,omitempty"`
	IncludeSensitiveInputs bool                 `json:"include_sensitive_inputs,omitempty"`
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
	if options.Authority == nil {
		localAuthority := ReadWrite()
		options.Authority = &localAuthority
	}
	ws, err := openPreparedWorkspace(options.WorkspaceOptions, root)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		closeErr := ws.Close()
		destroyErr := ws.Destroy(context.Background())
		if closeErr != nil || destroyErr != nil {
			resultErr = fmt.Errorf("%w (cleanup close: %v; cleanup destroy: %v)", resultErr, closeErr, destroyErr)
		}
	}()

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
	baseIdentity, err := workspaceFingerprint(root)
	if err != nil {
		return nil, fmt.Errorf("stow: fingerprint prepared inputs: %w", err)
	}
	committed = true
	return &PreparedWorkspace{Workspace: ws, WorkingDirectory: workingDirectory, SeededBytes: bytes, SeededObjects: objects, BaseIdentity: baseIdentity, Repositories: repositories}, nil
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
	options.owned = true
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
	var totalBytes, totalObjects int64
	for _, input := range inputs {
		if strings.TrimSpace(input.Source) == "" {
			return 0, 0, fmt.Errorf("stow: input source is required")
		}
		destination, err := safeDestination(root, input.Destination)
		if err != nil {
			return 0, 0, fmt.Errorf("stow: invalid input destination %q: %w", input.Destination, err)
		}
		if err := copySeed(input.Source, destination, includeSensitive, &totalBytes, &totalObjects); err != nil {
			return 0, 0, fmt.Errorf("stow: seed %q: %w", input.Source, err)
		}
	}
	return totalBytes, totalObjects, nil
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

func safeDestination(root, destination string) (string, error) {
	if destination == "" {
		destination = "."
	}
	if filepath.IsAbs(destination) || strings.Contains(destination, `\`) {
		return "", fmt.Errorf("must be relative")
	}
	for _, segment := range strings.Split(filepath.ToSlash(destination), "/") {
		if segment == ".." {
			return "", fmt.Errorf("must not contain parent-directory traversal")
		}
	}
	clean := filepath.Clean(destination)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("must not escape the workspace")
	}
	first := strings.Split(clean, string(filepath.Separator))[0]
	if strings.EqualFold(first, ".stow") {
		return "", fmt.Errorf(".stow is reserved")
	}
	resolved := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("must not escape the workspace")
	}
	return resolved, nil
}

func copySeed(source, destination string, includeSensitive bool, totalBytes, totalObjects *int64) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symbolic link inputs are not supported")
	}
	if info.IsDir() {
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return err
		}
		return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == source {
				return nil
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			target := filepath.Join(destination, rel)
			entryInfo, err := entry.Info()
			if err != nil {
				return err
			}
			if entryInfo.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("symbolic link input %s is not supported", path)
			}
			if entry.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			if !includeSensitive && sensitiveSeedPath(rel) {
				return fmt.Errorf("sensitive-looking input %s is excluded by default (set include_sensitive_inputs explicitly to include it)", rel)
			}
			return copySeedFile(path, target, entryInfo, totalBytes, totalObjects)
		})
	}
	if !includeSensitive && sensitiveSeedPath(filepath.Base(source)) {
		return fmt.Errorf("sensitive-looking input %s is excluded by default (set include_sensitive_inputs explicitly to include it)", filepath.Base(source))
	}
	return copySeedFile(source, destination, info, totalBytes, totalObjects)
}

func sensitiveSeedPath(path string) bool {
	clean := strings.ToLower(filepath.ToSlash(path))
	base := filepath.Base(clean)
	return sensitiveSeedBasename(base) || sensitiveSeedLocation(clean)
}

func sensitiveSeedBasename(base string) bool {
	return base == ".env" || strings.HasPrefix(base, ".env.") ||
		base == ".npmrc" || base == ".netrc" || base == "credentials" ||
		base == "credentials.json" || base == "service-account.json" ||
		base == "id_rsa" || strings.HasPrefix(base, "id_rsa.") ||
		base == "id_ed25519" || strings.HasPrefix(base, "id_ed25519.") ||
		strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") ||
		strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx")
}

func sensitiveSeedLocation(clean string) bool {
	return hasPathPrefix(clean, ".aws/credentials") || hasPathSegment(clean, ".aws/credentials") ||
		hasPathPrefix(clean, ".ssh/") || hasPathSegment(clean, ".ssh/") ||
		hasPathPrefix(clean, ".config/gcloud/credentials.db") || hasPathSegment(clean, ".config/gcloud/credentials.db")
}

func hasPathPrefix(path, prefix string) bool {
	return strings.HasPrefix(path, prefix)
}

func hasPathSegment(path, segment string) bool {
	return strings.Contains(path, "/"+segment)
}

func copySeedFile(source, destination string, info os.FileInfo, totalBytes, totalObjects *int64) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("only regular files and directories are supported: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()&0o755)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	maxInt64 := int64(^uint64(0) >> 1)
	if n > maxInt64-*totalBytes || *totalObjects == maxInt64 {
		return fmt.Errorf("seeded totals exceed supported limits")
	}
	*totalBytes += n
	(*totalObjects)++
	return nil
}
