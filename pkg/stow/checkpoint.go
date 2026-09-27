package stow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const checkpointVersion = 1

type CheckpointOptions struct {
	ParentID              string
	MaxBytes              int64
	MaxFiles              int64
	IncludeSensitiveFiles bool
}

type CheckpointFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}

type CheckpointManifest struct {
	Version     int              `json:"version"`
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspace_id"`
	ParentID    string           `json:"parent_id,omitempty"`
	Created     time.Time        `json:"created"`
	Files       []CheckpointFile `json:"files"`
	Excluded    []string         `json:"excluded,omitempty"`
}

type CheckpointInfo struct {
	ID          string
	WorkspaceID string
	ParentID    string
	Created     time.Time
	Files       int64
	Bytes       int64
	Excluded    []string
}

type CheckpointChange struct {
	Path string          `json:"path"`
	Kind string          `json:"kind"`
	From *CheckpointFile `json:"from,omitempty"`
	To   *CheckpointFile `json:"to,omitempty"`
}

// CreateCheckpoint captures immutable project files outside the mutable
// workspace. Stow metadata and Git internals are excluded; common credential
// filenames are excluded unless the caller explicitly opts in. The source tree
// is scanned before and after copying, and the operation refuses a changed tree.
func (w *Workspace) CreateCheckpoint(ctx context.Context, options CheckpointOptions) (CheckpointInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := w.assertOpen(); err != nil {
		return CheckpointInfo{}, err
	}
	w.checkpointMu.Lock()
	defer w.checkpointMu.Unlock()
	if err := ctx.Err(); err != nil {
		return CheckpointInfo{}, err
	}
	if options.ParentID != "" {
		if err := validateCheckpointParent(w.registryDir, w.id, options.ParentID); err != nil {
			return CheckpointInfo{}, err
		}
	}
	before, excluded, size, err := checkpointInputs(w.dir, options)
	if err != nil {
		return CheckpointInfo{}, err
	}
	if err := w.checkCheckpointRetention(size); err != nil {
		return CheckpointInfo{}, err
	}
	checkpointRoot, tempDir, err := stageCheckpointDirectory(w.registryDir)
	if err != nil {
		return CheckpointInfo{}, err
	}
	defer os.RemoveAll(tempDir)
	if err := copyCheckpointFiles(ctx, w.dir, tempDir, before); err != nil {
		return CheckpointInfo{}, err
	}
	if err := verifyCheckpointCapture(w.dir, before, excluded, options.IncludeSensitiveFiles); err != nil {
		return CheckpointInfo{}, err
	}
	id, err := newCheckpointID()
	if err != nil {
		return CheckpointInfo{}, err
	}
	manifest := CheckpointManifest{
		Version: checkpointVersion, ID: id, WorkspaceID: w.id,
		ParentID: options.ParentID, Created: w.nowFunc()().UTC(),
		Files: before, Excluded: excluded,
	}
	if err := publishCheckpoint(tempDir, checkpointRoot, id, manifest); err != nil {
		return CheckpointInfo{}, err
	}
	return CheckpointInfo{ID: id, WorkspaceID: w.id, ParentID: options.ParentID, Created: manifest.Created, Files: int64(len(before)), Bytes: size, Excluded: excluded}, nil
}

// checkCheckpointRetention accounts only published checkpoints. The caller
// holds checkpointMu through publication, so parallel captures on this handle
// cannot both pass the same remaining capacity.
func (w *Workspace) checkCheckpointRetention(nextBytes int64) error {
	if w.maxCheckpointBytes == 0 && w.maxCheckpoints == 0 {
		return nil
	}
	count, total, err := checkpointRetentionUsage(w.registryDir, w.id)
	if err != nil {
		return err
	}
	if w.maxCheckpoints > 0 && count >= w.maxCheckpoints {
		return fmt.Errorf("stow: checkpoint count limit reached (%d of %d); remove a checkpoint or raise the workspace limit", count, w.maxCheckpoints)
	}
	if w.maxCheckpointBytes > 0 && (total > w.maxCheckpointBytes || nextBytes > w.maxCheckpointBytes-total) {
		return fmt.Errorf("stow: checkpoint byte limit exceeded (%d existing + %d new > %d); remove a checkpoint or raise the workspace limit", total, nextBytes, w.maxCheckpointBytes)
	}
	return nil
}

func checkpointRetentionUsage(registryDir, workspaceID string) (int64, int64, error) {
	root := filepath.Join(registryDir, "checkpoints")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		entries = nil
	} else if err != nil {
		return 0, 0, fmt.Errorf("stow: inspect checkpoint retention: %w", err)
	}
	var count, total int64
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".checkpoint-") {
			continue
		}
		manifest, err := LoadCheckpoint(registryDir, entry.Name())
		if err != nil {
			return 0, 0, fmt.Errorf("stow: cannot enforce checkpoint retention because checkpoint %s is invalid: %w", entry.Name(), err)
		}
		if manifest.WorkspaceID != workspaceID {
			continue
		}
		if count == int64(^uint64(0)>>1) {
			return 0, 0, fmt.Errorf("stow: checkpoint count accounting overflow")
		}
		count++
		for _, file := range manifest.Files {
			if file.Size < 0 || total > int64(^uint64(0)>>1)-file.Size {
				return 0, 0, fmt.Errorf("stow: checkpoint byte accounting overflow")
			}
			total += file.Size
		}
	}
	return count, total, nil
}

func checkpointInputs(root string, options CheckpointOptions) ([]CheckpointFile, []string, int64, error) {
	if options.MaxBytes < 0 || options.MaxFiles < 0 {
		return nil, nil, 0, fmt.Errorf("stow: checkpoint limits must not be negative")
	}
	files, excluded, size, err := scanCheckpointFiles(root, options.IncludeSensitiveFiles)
	if err != nil {
		return nil, nil, 0, err
	}
	if options.MaxFiles > 0 && int64(len(files)) > options.MaxFiles {
		return nil, nil, 0, fmt.Errorf("stow: checkpoint files %d exceed limit %d", len(files), options.MaxFiles)
	}
	if options.MaxBytes > 0 && size > options.MaxBytes {
		return nil, nil, 0, fmt.Errorf("stow: checkpoint bytes %d exceed limit %d", size, options.MaxBytes)
	}
	return files, excluded, size, nil
}

func stageCheckpointDirectory(registryDir string) (string, string, error) {
	checkpointRoot := filepath.Join(registryDir, "checkpoints")
	if err := os.MkdirAll(checkpointRoot, 0o700); err != nil {
		return "", "", fmt.Errorf("stow: create checkpoint store: %w", err)
	}
	tempDir, err := os.MkdirTemp(checkpointRoot, ".checkpoint-")
	if err != nil {
		return "", "", fmt.Errorf("stow: stage checkpoint: %w", err)
	}
	return checkpointRoot, tempDir, nil
}

func verifyCheckpointCapture(root string, expected []CheckpointFile, excluded []string, includeSensitive bool) error {
	actual, actualExcluded, _, err := scanCheckpointFiles(root, includeSensitive)
	if err != nil {
		return err
	}
	if !sameCheckpointFiles(expected, actual) || !sameStrings(excluded, actualExcluded) {
		return fmt.Errorf("stow: workspace changed during checkpoint capture")
	}
	return nil
}

func publishCheckpoint(tempDir, checkpointRoot, id string, manifest CheckpointManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("stow: encode checkpoint: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "manifest.json"), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("stow: write checkpoint manifest: %w", err)
	}
	if err := os.Rename(tempDir, filepath.Join(checkpointRoot, id)); err != nil {
		return fmt.Errorf("stow: publish checkpoint: %w", err)
	}
	return nil
}

func sameCheckpointFiles(a, b []CheckpointFile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func newCheckpointID() (string, error) {
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("stow: create checkpoint id: %w", err)
	}
	return "cp_" + hex.EncodeToString(id[:]), nil
}

func validateCheckpointParent(registryDir, workspaceID, parentID string) error {
	parent, err := LoadCheckpoint(registryDir, parentID)
	if err != nil {
		return fmt.Errorf("stow: invalid parent checkpoint: %w", err)
	}
	if parent.WorkspaceID != workspaceID {
		return fmt.Errorf("stow: parent checkpoint belongs to a different workspace")
	}
	return nil
}

func LoadCheckpoint(registryDir, id string) (CheckpointManifest, error) {
	path, err := checkpointDirectory(registryDir, id)
	if err != nil {
		return CheckpointManifest{}, err
	}
	data, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return CheckpointManifest{}, fmt.Errorf("stow: read checkpoint %s: %w", id, err)
	}
	var manifest CheckpointManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return CheckpointManifest{}, fmt.Errorf("stow: decode checkpoint %s: %w", id, err)
	}
	if manifest.Version != checkpointVersion || manifest.ID != id {
		return CheckpointManifest{}, fmt.Errorf("stow: checkpoint %s has an unsupported or mismatched manifest", id)
	}
	if err := validateCheckpointManifest(manifest); err != nil {
		return CheckpointManifest{}, err
	}
	return manifest, nil
}

func validateCheckpointManifest(manifest CheckpointManifest) error {
	if manifest.WorkspaceID == "" {
		return fmt.Errorf("stow: checkpoint manifest has no workspace ID")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		if !validCheckpointPath(file.Path) {
			return fmt.Errorf("stow: checkpoint manifest contains unsafe path %q", file.Path)
		}
		if _, found := seen[file.Path]; found {
			return fmt.Errorf("stow: checkpoint manifest repeats path %q", file.Path)
		}
		seen[file.Path] = struct{}{}
		if !validCheckpointFileMetadata(file) {
			return fmt.Errorf("stow: checkpoint manifest has invalid metadata for %q", file.Path)
		}
	}
	return nil
}

func validCheckpointPath(path string) bool {
	if path == "" || path == "." || filepath.IsAbs(path) || filepath.VolumeName(path) != "" || strings.Contains(path, `\`) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean != path || strings.HasPrefix(path, "../") || path == ".." {
		return false
	}
	return validCheckpointPathSegments(path)
}

func validCheckpointPathSegments(path string) bool {
	for i, segment := range strings.Split(path, "/") {
		if segment == "" || strings.Contains(segment, ":") || strings.EqualFold(segment, ".git") {
			return false
		}
		if !validPortablePathSegment(segment) {
			return false
		}
		if i == 0 && strings.EqualFold(segment, ".stow") {
			return false
		}
	}
	return true
}

func validPortablePathSegment(segment string) bool {
	if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") || strings.ContainsAny(segment, `<>:"|?*`) {
		return false
	}
	for _, char := range segment {
		if char < 0x20 {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(segment, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return true
}

func validCheckpointFileMetadata(file CheckpointFile) bool {
	if file.Size < 0 || len(file.SHA256) != 64 {
		return false
	}
	_, err := hex.DecodeString(file.SHA256)
	return err == nil
}

func CompareCheckpoints(registryDir, fromID, toID string) ([]CheckpointChange, error) {
	from, err := LoadCheckpoint(registryDir, fromID)
	if err != nil {
		return nil, err
	}
	to, err := LoadCheckpoint(registryDir, toID)
	if err != nil {
		return nil, err
	}
	return diffCheckpointFiles(from.Files, to.Files), nil
}

func diffCheckpointFiles(from, to []CheckpointFile) []CheckpointChange {
	before := make(map[string]CheckpointFile, len(from))
	after := make(map[string]CheckpointFile, len(to))
	for _, file := range from {
		before[file.Path] = file
	}
	for _, file := range to {
		after[file.Path] = file
	}
	paths := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		paths[path] = struct{}{}
	}
	for path := range after {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var changes []CheckpointChange
	for _, path := range ordered {
		old, hadOld := before[path]
		current, hasCurrent := after[path]
		switch {
		case !hadOld:
			copy := current
			changes = append(changes, CheckpointChange{Path: path, Kind: "added", To: &copy})
		case !hasCurrent:
			copy := old
			changes = append(changes, CheckpointChange{Path: path, Kind: "deleted", From: &copy})
		case old.SHA256 != current.SHA256 || old.Mode != current.Mode:
			oldCopy, newCopy := old, current
			changes = append(changes, CheckpointChange{Path: path, Kind: "changed", From: &oldCopy, To: &newCopy})
		}
	}
	return changes
}

func RestoreCheckpoint(registryDir, id string, options WorkspaceOptions) (*Workspace, error) {
	manifest, err := LoadCheckpoint(registryDir, id)
	if err != nil {
		return nil, err
	}
	checkpointDir, err := checkpointDirectory(registryDir, id)
	if err != nil {
		return nil, err
	}
	inputs := make([]WorkspaceInput, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		if _, err := safeDestination(options.Dir, file.Path); err != nil {
			return nil, fmt.Errorf("stow: unsafe path in checkpoint: %w", err)
		}
		source := filepath.Join(checkpointDir, "files", filepath.FromSlash(file.Path))
		digest, size, err := digestFile(source)
		if err != nil || digest != file.SHA256 || size != file.Size {
			return nil, fmt.Errorf("stow: checkpoint file %q failed integrity validation", file.Path)
		}
		inputs = append(inputs, WorkspaceInput{
			Source:      source,
			Destination: file.Path,
		})
	}
	if len(inputs) == 0 {
		root, err := validatePrepareRoot(options.Dir)
		if err != nil {
			return nil, err
		}
		if options.Authority == nil {
			localAuthority := ReadWrite()
			options.Authority = &localAuthority
		}
		return openPreparedWorkspace(options, root)
	}
	prepared, err := PrepareWorkspace(PrepareOptions{
		WorkspaceOptions: options, Inputs: inputs, IncludeSensitiveInputs: true,
	})
	if err != nil {
		return nil, err
	}
	return prepared.Workspace, nil
}

func checkpointDirectory(registryDir, id string) (string, error) {
	if !validCheckpointID(id) {
		return "", fmt.Errorf("stow: invalid checkpoint id %q", id)
	}
	if registryDir == "" {
		var err error
		registryDir, err = DefaultWorkspaceRegistryDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(registryDir, "checkpoints", id), nil
}

func validCheckpointID(id string) bool {
	if len(id) != 27 || !strings.HasPrefix(id, "cp_") {
		return false
	}
	for _, char := range id[3:] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
