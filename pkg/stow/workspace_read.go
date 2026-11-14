package stow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

const (
	defaultWorkspaceReadMaxFileBytes = 64 * 1024
	defaultWorkspaceReadMaxEntries   = 100
	maxWorkspaceReadFileBytes        = 64 * 1024
	maxWorkspaceReadEntries          = 100
	maxWorkspaceReadScannedEntries   = 10_000
	maxWorkspaceObservationEntries   = 10_000
	maxWorkspaceObservationBytes     = 4 << 20
	maxWorkspaceReadPathBytes        = 1_024
	maxWorkspaceManifestBytes        = 64 * 1024
)

var (
	ErrWorkspaceReaderClosed    = errors.New("stow: workspace reader is closed")
	ErrWorkspaceReadUnavailable = errors.New("stow: workspace resource is unavailable")
	ErrWorkspaceReadTooLarge    = errors.New("stow: workspace resource is too large")
	ErrWorkspaceReadChanged     = errors.New("stow: workspace resource changed during read")
)

// WorkspaceReaderOptions selects one registered workspace and narrows its read bounds.
type WorkspaceReaderOptions struct {
	RegistryDir  string
	WorkspaceID  string
	MaxFileBytes int64
	MaxEntries   int
	ReadRoots    []string
}

// WorkspaceReadEntry describes one direct child of a directory.
type WorkspaceReadEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size"`
}

// WorkspaceReadFile contains bounded current bytes from the live working view.
type WorkspaceReadFile struct {
	Path        string
	Data        []byte
	ContentType string
	Modified    time.Time
}

// WorkspaceReader is a confined, read-only attachment to one registered workspace.
type WorkspaceReader struct {
	mu         sync.Mutex
	reference  WorkspaceReference
	root       *rooted.Root
	lease      *workspace.CaptureLock
	maxBytes   int64
	maxEntries int
	readRoots  []string
	closed     bool
}

// OpenWorkspaceReader attaches to an existing workspace without opening a writer
// session or changing its manifest, index, registry entry or workspace identity.
func OpenWorkspaceReader(options WorkspaceReaderOptions) (*WorkspaceReader, error) {
	maxBytes, maxEntries, err := workspaceReaderBounds(options)
	if err != nil {
		return nil, err
	}
	registryDir, err := ResolveRegistryDir(options.RegistryDir, "")
	if err != nil {
		return nil, ErrWorkspaceReadUnavailable
	}
	lease, err := workspace.AcquireReadAttachment(registryDir, options.WorkspaceID)
	if err != nil {
		return nil, ErrWorkspaceReadUnavailable
	}
	cleanupLease := true
	defer func() {
		if cleanupLease {
			_ = lease.Release()
		}
	}()
	reference, err := LookupWorkspace(registryDir, options.WorkspaceID)
	if err != nil {
		return nil, ErrWorkspaceReadUnavailable
	}
	root, err := rooted.Open(reference.Dir)
	if err != nil {
		return nil, ErrWorkspaceReadUnavailable
	}
	cleanupRoot := true
	defer func() {
		if cleanupRoot {
			_ = root.Close()
		}
	}()
	readRoots, err := normalizeWorkspaceReadRoots(root, options.ReadRoots)
	if err != nil {
		return nil, err
	}
	if err := verifyWorkspaceReaderManifest(root, reference); err != nil {
		return nil, err
	}
	if err := root.Check(); err != nil {
		return nil, ErrWorkspaceReadUnavailable
	}
	reader := &WorkspaceReader{
		reference:  reference,
		root:       root,
		lease:      lease,
		maxBytes:   maxBytes,
		maxEntries: maxEntries,
		readRoots:  readRoots,
	}
	cleanupRoot, cleanupLease = false, false
	return reader, nil
}

func workspaceReaderBounds(options WorkspaceReaderOptions) (int64, int, error) {
	maxBytes := options.MaxFileBytes
	if maxBytes == 0 {
		maxBytes = defaultWorkspaceReadMaxFileBytes
	}
	maxEntries := options.MaxEntries
	if maxEntries == 0 {
		maxEntries = defaultWorkspaceReadMaxEntries
	}
	if maxBytes < 1 || maxBytes > maxWorkspaceReadFileBytes || maxEntries < 1 || maxEntries > maxWorkspaceReadEntries {
		return 0, 0, ErrWorkspaceReadUnavailable
	}
	return maxBytes, maxEntries, nil
}

func verifyWorkspaceReaderManifest(root *rooted.Root, reference WorkspaceReference) error {
	if err := root.Check(); err != nil {
		return ErrWorkspaceReadUnavailable
	}
	file, err := root.OpenRegularFile(".stow/manifest.json")
	if err != nil {
		return ErrWorkspaceReadUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > maxWorkspaceManifestBytes {
		return ErrWorkspaceReadUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxWorkspaceManifestBytes+1))
	if err != nil || int64(len(raw)) > maxWorkspaceManifestBytes {
		return ErrWorkspaceReadUnavailable
	}
	var manifest workspace.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Version != 3 || manifest.WorkspaceID != reference.ID || manifest.Bucket != reference.Bucket {
		return ErrWorkspaceReadUnavailable
	}
	if err := root.Check(); err != nil {
		return ErrWorkspaceReadUnavailable
	}
	return nil
}

// Reference returns the resolved host-side identity and paths.
func (r *WorkspaceReader) Reference() WorkspaceReference { return r.reference }

// Close releases the root and shared read lease. It is safe to call more than once.
func (r *WorkspaceReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	rootErr := r.root.Close()
	leaseErr := r.lease.Release()
	if rootErr != nil {
		return rootErr
	}
	return leaseErr
}

// ReadFile returns the current bytes of a regular workspace file, within the
// configured decoded-byte bound.
func (r *WorkspaceReader) ReadFile(ctx context.Context, path string) (WorkspaceReadFile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return WorkspaceReadFile{}, ErrWorkspaceReaderClosed
	}
	if err := ctx.Err(); err != nil {
		return WorkspaceReadFile{}, err
	}
	if !validWorkspaceReadPath(path, false) || workspaceReadExcluded(path) || !workspaceReadFileAllowed(path, r.readRoots) {
		return WorkspaceReadFile{}, ErrWorkspaceReadUnavailable
	}
	if err := r.root.Check(); err != nil {
		return WorkspaceReadFile{}, ErrWorkspaceReadUnavailable
	}
	file, err := r.root.OpenRegularFile(path)
	if err != nil {
		return WorkspaceReadFile{}, ErrWorkspaceReadUnavailable
	}
	defer file.Close()
	return readWorkspaceFile(ctx, path, file, r.root, r.maxBytes)
}

func readWorkspaceFile(ctx context.Context, path string, file *os.File, root *rooted.Root, maxBytes int64) (WorkspaceReadFile, error) {
	before, err := file.Stat()
	if err != nil {
		return WorkspaceReadFile{}, ErrWorkspaceReadUnavailable
	}
	if before.Size() > maxBytes {
		return WorkspaceReadFile{}, ErrWorkspaceReadTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return WorkspaceReadFile{}, ErrWorkspaceReadUnavailable
	}
	if int64(len(data)) > maxBytes {
		return WorkspaceReadFile{}, ErrWorkspaceReadTooLarge
	}
	if err := ctx.Err(); err != nil {
		return WorkspaceReadFile{}, err
	}
	after, err := file.Stat()
	if err != nil || workspaceReadFileChanged(before, after, data) {
		return WorkspaceReadFile{}, ErrWorkspaceReadChanged
	}
	if err := root.Check(); err != nil {
		return WorkspaceReadFile{}, ErrWorkspaceReadChanged
	}
	contentType := http.DetectContentType(data[:min(len(data), 512)])
	return WorkspaceReadFile{Path: path, Data: data, ContentType: contentType, Modified: after.ModTime().UTC()}, nil
}

func workspaceReadFileChanged(before, after os.FileInfo, data []byte) bool {
	return !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || int64(len(data)) != after.Size()
}

// ReadDirectory returns a complete, sorted observation of one directory's direct
// visible children. It never returns partial results on overflow.
func (r *WorkspaceReader) ReadDirectory(ctx context.Context, path string) ([]WorkspaceReadEntry, error) {
	return r.readWorkspaceDirectory(ctx, path)
}

func validWorkspaceReadPath(path string, allowRoot bool) bool {
	if path == "." {
		return allowRoot
	}
	if path == "" || len(path) > maxWorkspaceReadPathBytes || strings.ContainsAny(path, "\\\x00") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || !validWorkspaceReadSegment(part) {
			return false
		}
	}
	return true
}

func validWorkspaceReadSegment(part string) bool {
	if !utf8.ValidString(part) {
		return false
	}
	for _, r := range part {
		if unicode.IsControl(r) {
			return false
		}
	}
	return part != ""
}

func workspaceReadExcluded(path string) bool {
	segments := strings.Split(workspaceReadFold(path), "/")
	for index, segment := range segments {
		switch segment {
		case ".stow", ".git", ".ssh", ".aws":
			return true
		}
		if sensitiveSeedPath(strings.Join(segments[:index+1], "/")) {
			return true
		}
	}
	return false
}

func workspaceReadFold(path string) string {
	return strings.ToLower(strings.ToUpper(path))
}
