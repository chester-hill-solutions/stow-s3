package stow

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	workspaceStore "github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

const (
	workspaceFileCoordinationHostQuiesced = "host_quiesced"
	defaultWorkspaceEditorMaxBytes        = int64(64 * 1024)
	maxWorkspaceEditorBytes               = int64(64 * 1024)
	defaultWorkspaceEditorObservations    = 64
	maxWorkspaceEditorObservations        = 64
	defaultWorkspaceEditorLifetime        = 5 * time.Minute
)

var (
	ErrWorkspaceEditorUnavailable  = errors.New("stow: workspace file editor unavailable")
	ErrWorkspaceEditorClosed       = errors.New("stow: workspace file editor is closed")
	ErrWorkspaceObservationFull    = errors.New("stow: workspace file observation capacity reached")
	ErrWorkspaceObservationInvalid = errors.New("stow: workspace file observation is invalid or expired")
)

type WorkspaceEditOptions struct {
	ReadRoots           []string
	WriteRoots          []string
	Coordination        string
	MaxFileBytes        int64
	MaxObservations     int
	ObservationLifetime time.Duration
}

type workspaceFileBasis struct {
	incarnation string
	path        string
	exists      bool
	digest      string
	size        int64
	mode        uint32
	modified    time.Time
	generation  string
	identity    storage.WorkspaceFileIdentity
	metadata    string
	expires     time.Time
}

type WorkspaceEditor struct {
	owner        *Workspace
	incarnation  string
	readRoots    []string
	writeRoots   []string
	maxBytes     int64
	maxLive      int
	lifetime     time.Duration
	observations map[string]workspaceFileBasis
	closed       bool
}

func (w *Workspace) OpenFileEditor(options WorkspaceEditOptions) (*WorkspaceEditor, error) {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if w.lifecycle != workspaceOpen || w.policyConfigured || w.editor != nil || options.Coordination != workspaceFileCoordinationHostQuiesced {
		return nil, ErrWorkspaceEditorUnavailable
	}
	maxBytes, maxLive, lifetime, err := workspaceEditorBounds(options)
	if err != nil {
		return nil, err
	}
	lock, err := workspaceStore.AcquireMutationCapture(w.registryDir, w.id)
	if err != nil {
		return nil, ErrWorkspaceEditorUnavailable
	}
	defer lock.Release()
	if err := w.instance.CheckWorkspaceFileRead(w.bucket, ""); err != nil {
		return nil, ErrWorkspaceEditorUnavailable
	}
	if err := w.store.CheckWorkspaceFileSaveEditorOpen(); err != nil {
		return nil, ErrWorkspaceEditorUnavailable
	}
	root, err := rooted.Open(w.dir)
	if err != nil {
		return nil, ErrWorkspaceEditorUnavailable
	}
	defer root.Close()
	readRoots, err := normalizeWorkspaceReadRoots(root, options.ReadRoots)
	if err != nil {
		return nil, ErrWorkspaceEditorUnavailable
	}
	writeRoots, err := normalizeEditorWriteRoots(root, options.WriteRoots, readRoots)
	if err != nil {
		return nil, err
	}
	incarnation, err := newWorkspaceEditorIncarnation()
	if err != nil {
		return nil, err
	}
	editor := &WorkspaceEditor{owner: w, incarnation: incarnation, readRoots: readRoots, writeRoots: writeRoots,
		maxBytes: maxBytes, maxLive: maxLive, lifetime: lifetime, observations: make(map[string]workspaceFileBasis)}
	w.editor = editor
	return editor, nil
}

func workspaceEditorBounds(options WorkspaceEditOptions) (int64, int, time.Duration, error) {
	maxBytes, maxLive, lifetime := options.MaxFileBytes, options.MaxObservations, options.ObservationLifetime
	if maxBytes == 0 {
		maxBytes = defaultWorkspaceEditorMaxBytes
	}
	if maxLive == 0 {
		maxLive = defaultWorkspaceEditorObservations
	}
	if lifetime == 0 {
		lifetime = defaultWorkspaceEditorLifetime
	}
	if maxBytes < 1 || maxBytes > maxWorkspaceEditorBytes || maxLive < 1 || maxLive > maxWorkspaceEditorObservations || lifetime <= 0 || lifetime > defaultWorkspaceEditorLifetime {
		return 0, 0, 0, ErrWorkspaceEditorUnavailable
	}
	return maxBytes, maxLive, lifetime, nil
}

func normalizeEditorWriteRoots(root *rooted.Root, configured, readRoots []string) ([]string, error) {
	if len(configured) == 0 {
		return nil, ErrWorkspaceEditorUnavailable
	}
	roots, err := normalizeWorkspaceReadRoots(root, configured)
	if err != nil {
		return nil, ErrWorkspaceEditorUnavailable
	}
	for _, writeRoot := range roots {
		allowed := false
		for _, readRoot := range readRoots {
			if workspaceReadWithin(readRoot, writeRoot) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, ErrWorkspaceEditorUnavailable
		}
	}
	sort.Strings(roots)
	return roots, nil
}

func newWorkspaceEditorIncarnation() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("stow: create workspace editor: %w", err)
	}
	return hex.EncodeToString(id[:]), nil
}

func (e *WorkspaceEditor) Close() error {
	w := e.owner
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	e.closeLocked()
	return nil
}

func (e *WorkspaceEditor) closeLocked() {
	if e.closed {
		return
	}
	e.closed = true
	clear(e.observations)
	if e.owner.editor == e {
		e.owner.editor = nil
	}
}

func (e *WorkspaceEditor) validLocked() bool {
	return !e.closed && e.owner.lifecycle == workspaceOpen && e.owner.editor == e
}
