package stow

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
)

const checkpointVersion = 1

type CheckpointOptions struct {
	PortableObjects       bool   `json:"portable_objects,omitempty"`
	ParentID              string `json:"parent_id,omitempty"`
	MaxBytes              int64  `json:"max_bytes,omitempty"`
	MaxFiles              int64  `json:"max_files,omitempty"`
	IncludeSensitiveFiles bool   `json:"include_sensitive_files,omitempty"`
}

type CheckpointFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}

type CheckpointManifest struct {
	Provenance       *CheckpointProvenance `json:"provenance,omitempty"`
	PrimaryBucket    string                `json:"primary_bucket,omitempty"`
	Buckets          []string              `json:"buckets,omitempty"`
	Objects          []CheckpointObject    `json:"objects,omitempty"`
	Payloads         []CheckpointFile      `json:"payloads,omitempty"`
	WorkingDirectory string                `json:"working_directory,omitempty"`
	Version          int                   `json:"version"`
	ID               string                `json:"id"`
	WorkspaceID      string                `json:"workspace_id"`
	ParentID         string                `json:"parent_id,omitempty"`
	Created          time.Time             `json:"created"`
	Files            []CheckpointFile      `json:"files"`
	Excluded         []string              `json:"excluded,omitempty"`
}

type CheckpointInfo struct {
	Version     int       `json:"version,omitempty"`
	Objects     int64     `json:"objects,omitempty"`
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	ParentID    string    `json:"parent_id"`
	Created     time.Time `json:"created"`
	Files       int64     `json:"files"`
	Bytes       int64     `json:"bytes"`
	Excluded    []string  `json:"excluded"`
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
//
// It requires the handle, and so it requires that no other process holds the
// workspace. A caller that needs to snapshot a workspace somebody is using right
// now has CheckpointOf, which is the same capture without the session.
func (w *Workspace) CreateCheckpoint(ctx context.Context, options CheckpointOptions) (CheckpointInfo, error) {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if w.closed || w.closing {
		return CheckpointInfo{}, ErrClosed
	}
	lock, err := workspace.AcquireMutationCapture(w.registryDir, w.id)
	if err != nil {
		return CheckpointInfo{}, err
	}
	defer lock.Release()
	if err := ctx.Err(); err != nil {
		return CheckpointInfo{}, err
	}
	return captureCheckpoint(ctx, w.captureTarget(), options)
}

// checkCheckpointRetention accounts only published checkpoints. The caller
// holds the capture gate through publication, so parallel captures
// cannot both pass the same remaining capacity.
func (w *Workspace) checkCheckpointRetention(nextBytes int64) error {
	return w.captureTarget().checkRetention(nextBytes)
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
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".checkpoint-") || strings.HasPrefix(entry.Name(), ".import-") {
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
		for _, file := range checkpointPayloadFiles(manifest) {
			if file.Size < 0 || total > int64(^uint64(0)>>1)-file.Size {
				return 0, 0, fmt.Errorf("stow: checkpoint byte accounting overflow")
			}
			total += file.Size
		}
	}
	return count, total, nil
}

func checkpointInputs(ctx context.Context, root *rooted.Root, options CheckpointOptions) ([]CheckpointFile, []string, int64, error) {
	if options.MaxBytes < 0 || options.MaxFiles < 0 {
		return nil, nil, 0, fmt.Errorf("stow: checkpoint limits must not be negative")
	}
	files, excluded, size, err := scanCheckpointRoot(ctx, root, options.IncludeSensitiveFiles)
	if err != nil {
		return nil, nil, 0, err
	}
	if options.MaxFiles > 0 && int64(len(files)) > options.MaxFiles {
		return nil, nil, 0, checkpointFailure("capacity_exceeded", "scan", "not_committed", fmt.Errorf("checkpoint files %d exceed limit %d", len(files), options.MaxFiles))
	}
	if options.MaxBytes > 0 && size > options.MaxBytes {
		return nil, nil, 0, checkpointFailure("capacity_exceeded", "scan", "not_committed", fmt.Errorf("checkpoint bytes %d exceed limit %d", size, options.MaxBytes))
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

func verifyCheckpointCapture(ctx context.Context, root *rooted.Root, expected []CheckpointFile, excluded []string, includeSensitive bool) error {
	actual, actualExcluded, _, err := scanCheckpointRoot(ctx, root, includeSensitive)
	if err != nil {
		return err
	}
	if !sameCheckpointFiles(expected, actual) || !sameStrings(excluded, actualExcluded) {
		return checkpointFailure("workspace_changed", "verify", "not_committed", fmt.Errorf("workspace changed during checkpoint capture"))
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
	input, err := os.Open(filepath.Join(path, "manifest.json"))
	if err != nil {
		return CheckpointManifest{}, err
	}
	defer input.Close()
	data, err := io.ReadAll(io.LimitReader(input, maxCheckpointManifestSize+1))
	if len(data) > maxCheckpointManifestSize {
		return CheckpointManifest{}, fmt.Errorf("stow: checkpoint manifest exceeds size limit")
	}
	if err != nil {
		return CheckpointManifest{}, fmt.Errorf("stow: read checkpoint %s: %w", id, err)
	}
	var manifest CheckpointManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return CheckpointManifest{}, fmt.Errorf("stow: decode checkpoint %s: %w", id, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return CheckpointManifest{}, fmt.Errorf("stow: trailing checkpoint manifest data")
	}
	if !supportedCheckpointVersion(manifest.Version) || manifest.ID != id {
		return CheckpointManifest{}, fmt.Errorf("stow: checkpoint %s has an unsupported or mismatched manifest", id)
	}
	if err := validateCheckpointManifest(manifest); err != nil {
		return CheckpointManifest{}, err
	}
	return manifest, nil
}

func validateCheckpointManifest(manifest CheckpointManifest) error {
	if !supportedCheckpointVersion(manifest.Version) {
		return fmt.Errorf("stow: unsupported checkpoint version")
	}
	if err := validatePortableManifest(manifest); err != nil {
		return err
	}
	if !workspace.ValidWorkspaceID(manifest.WorkspaceID) {
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
	if file.Size < 0 || len(file.SHA256) != 64 || file.Mode > 0o777 {
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
	if from.Version != checkpointVersion || to.Version != checkpointVersion {
		return nil, fmt.Errorf("stow: use ComparePortableCheckpoints for portable object checkpoints")
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
