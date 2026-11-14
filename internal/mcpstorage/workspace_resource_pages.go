package mcpstorage

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	workspaceObservationMaxEntries             = 10_000
	workspaceObservationMaxBytes         int64 = 4 << 20
	workspaceObservationMaxCount               = 8
	workspaceObservationMaxCacheBytes    int64 = 16 << 20
	workspaceObservationPageSize               = 100
	workspaceObservationPageScratchBytes int64 = 512 << 10
	workspacePageScratchLimit                  = workspaceFilesLimit * workspaceObservationPageScratchBytes
)

var workspaceObservationTTL = 5 * time.Minute

type workspaceDirectoryPage struct {
	Version             int                   `json:"version"`
	WorkspaceID         string                `json:"workspace_id"`
	Path                string                `json:"path"`
	View                string                `json:"view"`
	ObservationID       string                `json:"observation_id"`
	ObservedAt          time.Time             `json:"observed_at"`
	ObservationComplete bool                  `json:"observation_complete"`
	Entries             []workspaceEntry      `json:"entries"`
	Page                workspacePageMetadata `json:"page"`
}

type workspacePageMetadata struct {
	Offset  int    `json:"offset"`
	Count   int    `json:"count"`
	NextURI string `json:"next_uri,omitempty"`
}

type workspacePageCursor struct {
	Version       int    `json:"v"`
	ObservationID string `json:"o"`
	Offset        int    `json:"n"`
}

type workspacePageObservation struct {
	id          string
	path        string
	observation *stow.WorkspaceDirectoryObservation
	expires     time.Time
	lastUsed    time.Time
	charge      int64
	active      int
	cached      bool
}

func workspacePageURI(id, path string) string {
	return "stow://workspaces/" + id + "/tree-pages/v1/" + base64.RawURLEncoding.EncodeToString([]byte(path))
}

func workspacePageNextURI(id, path string, cursor workspacePageCursor) string {
	encoded, _ := json.Marshal(cursor)
	return workspacePageURI(id, path) + "/" + base64.RawURLEncoding.EncodeToString(encoded)
}

func (host *WorkspaceFiles) isWorkspacePageRoute(uri string) bool {
	prefix := "stow://workspaces/" + host.resourceWorkspaceID() + "/tree-pages/"
	return strings.HasPrefix(uri, prefix)
}

func (host *WorkspaceFiles) parseWorkspacePageURI(uri string) (name string, cursor *workspacePageCursor, err error) {
	if !validWorkspacePageURI(uri) {
		return "", nil, workspaceNotFound()
	}
	prefix := host.workspacePagePrefix()
	if !strings.HasPrefix(uri, prefix) {
		return "", nil, workspaceNotFound()
	}
	parts := strings.Split(strings.TrimPrefix(uri, prefix), "/")
	if len(parts) == 0 || len(parts) > 2 {
		return "", nil, workspaceNotFound()
	}
	name, ok := decodeWorkspacePagePath(parts[0])
	if !ok {
		return "", nil, workspaceNotFound()
	}
	if len(parts) == 1 {
		return name, nil, nil
	}
	decodedCursor, ok := host.decodeWorkspacePageCursor(uri, name, parts[1])
	if !ok {
		return "", nil, workspaceNotFound()
	}
	return name, &decodedCursor, nil
}

func (host *WorkspaceFiles) workspacePagePrefix() string {
	return "stow://workspaces/" + host.resourceWorkspaceID() + "/tree-pages/v1/"
}

func validWorkspacePageURI(uri string) bool {
	return len(uri) <= 2048 && !strings.ContainsAny(uri, "?#\\")
}

func decodeWorkspacePagePath(encoded string) (string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	name := string(decoded)
	return name, err == nil && validWorkspaceURIPath(decoded, name, "tree")
}

func (host *WorkspaceFiles) decodeWorkspacePageCursor(uri, name, encoded string) (workspacePageCursor, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return workspacePageCursor{}, false
	}
	var cursor workspacePageCursor
	if json.Unmarshal(decoded, &cursor) != nil || cursor.Version != 1 || cursor.Offset <= 0 || cursor.ObservationID == "" {
		return workspacePageCursor{}, false
	}
	return cursor, workspacePageNextURI(host.resourceWorkspaceID(), name, cursor) == uri
}

func (host *WorkspaceFiles) workspacePageContent(ctx context.Context, uri, id, name string, cursor *workspacePageCursor) (*mcp.ResourceContents, error) {
	entry, offset, err := host.getPageObservation(ctx, id, name, cursor)
	if err != nil {
		return nil, err
	}
	defer host.releasePageObservation(entry)
	if err := host.reservePageScratch(); err != nil {
		return nil, err
	}
	defer host.releasePageScratch()
	entries, err := entry.observation.Page(ctx, offset, workspaceObservationPageSize)
	if err != nil {
		return nil, workspaceReadError(err)
	}
	total := entry.observation.Len()
	if len(entries) > workspaceObservationPageSize || offset+len(entries) > total {
		return nil, workspaceNotFound()
	}
	for count := len(entries); count >= 0; count-- {
		if count == 0 && offset < total {
			break
		}
		result := host.buildWorkspaceDirectoryPage(workspacePageBuild{id: id, name: name, observation: entry, offset: offset, entries: entries[:count], total: total})
		content, err := marshalWorkspaceDirectoryPage(uri, result)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(&mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{content}})
		if err == nil && len(encoded) <= workspaceResponseLimit {
			return content, nil
		}
	}
	return nil, fmt.Errorf("workspace resource exceeds response limit")
}

type workspacePageBuild struct {
	id          string
	name        string
	observation *workspacePageObservation
	offset      int
	entries     []stow.WorkspaceReadEntry
	total       int
}

func (host *WorkspaceFiles) buildWorkspaceDirectoryPage(input workspacePageBuild) workspaceDirectoryPage {
	result := workspaceDirectoryPage{Version: 1, WorkspaceID: input.id, Path: input.name, View: "mutable working view", ObservationID: input.observation.id, ObservedAt: input.observation.observation.ObservedAt(), ObservationComplete: true, Entries: make([]workspaceEntry, 0, len(input.entries))}
	for _, entry := range input.entries {
		childURI := workspacePathURI("files", input.id, entry.Path)
		if entry.Kind == "directory" {
			childURI = workspacePageURI(input.id, entry.Path)
		}
		result.Entries = append(result.Entries, workspaceEntry{Name: entry.Name, Path: entry.Path, Kind: entry.Kind, Size: entry.Size, URI: childURI})
	}
	result.Page.Offset, result.Page.Count = input.offset, len(input.entries)
	if nextOffset := input.offset + len(input.entries); nextOffset < input.total {
		result.Page.NextURI = workspacePageNextURI(input.id, input.name, workspacePageCursor{Version: 1, ObservationID: input.observation.id, Offset: nextOffset})
	}
	return result
}

func marshalWorkspaceDirectoryPage(uri string, value workspaceDirectoryPage) (*mcp.ResourceContents, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, workspaceNotFound()
	}
	return &mcp.ResourceContents{URI: uri, MIMEType: "application/json", Text: string(body)}, nil
}

func (host *WorkspaceFiles) getPageObservation(ctx context.Context, id, name string, cursor *workspacePageCursor) (*workspacePageObservation, int, error) {
	if cursor != nil {
		return host.acquireCachedObservation(id, name, cursor)
	}
	if err := host.reserveObservationScan(); err != nil {
		return nil, 0, err
	}
	observation, err := host.reader.ObserveDirectory(ctx, name)
	if err != nil {
		host.releaseObservationScan()
		return nil, 0, workspaceReadError(err)
	}
	if bytes := observation.MetadataBytes(); bytes < 0 || bytes > workspaceObservationMaxBytes || observation.Len() > workspaceObservationMaxEntries {
		host.releaseObservationScan()
		return nil, 0, fmt.Errorf("workspace observation exceeds metadata bounds")
	}
	randomID := make([]byte, 16)
	if _, err := rand.Read(randomID); err != nil {
		host.releaseObservationScan()
		return nil, 0, fmt.Errorf("workspace observation unavailable")
	}
	entry := &workspacePageObservation{id: base64.RawURLEncoding.EncodeToString(randomID), path: name, observation: observation, charge: observation.MetadataBytes()}
	host.mu.Lock()
	defer host.mu.Unlock()
	host.pageMemoryBytes -= workspaceObservationMaxBytes
	host.pageScanning = false
	if host.closed {
		host.cond.Broadcast()
		return nil, 0, fmt.Errorf("workspace reader closed")
	}
	now := host.pageNow()
	entry.expires, entry.lastUsed, entry.cached, entry.active = now.Add(workspaceObservationTTL), now, true, 1
	host.pageMemoryBytes += entry.charge
	host.pageObservations[entry.id] = entry
	host.cond.Broadcast()
	return entry, 0, nil
}

func (host *WorkspaceFiles) reserveObservationScan() error {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.closed {
		return fmt.Errorf("workspace reader closed")
	}
	if host.pageScanning {
		return fmt.Errorf("workspace reader busy")
	}
	host.evictWorkspaceObservationsLocked(host.pageNow())
	for len(host.pageObservations) >= workspaceObservationMaxCount || host.pageMemoryBytes+workspaceObservationMaxBytes > workspaceObservationMaxCacheBytes {
		if !host.evictOldestWorkspaceObservationLocked() {
			return fmt.Errorf("workspace reader busy")
		}
	}
	host.pageScanning = true
	host.pageMemoryBytes += workspaceObservationMaxBytes
	return nil
}

func (host *WorkspaceFiles) acquireCachedObservation(id, name string, cursor *workspacePageCursor) (*workspacePageObservation, int, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.closed {
		return nil, 0, fmt.Errorf("workspace reader closed")
	}
	now := host.pageNow()
	host.evictWorkspaceObservationsLocked(now)
	entry := host.pageObservations[cursor.ObservationID]
	if entry == nil || entry.path != name || id != host.resourceWorkspaceID() || cursor.Offset >= entry.observation.Len() {
		return nil, 0, workspaceNotFound()
	}
	entry.active++
	entry.lastUsed = now
	return entry, cursor.Offset, nil
}

func (host *WorkspaceFiles) evictWorkspaceObservationsLocked(now time.Time) {
	for id, entry := range host.pageObservations {
		if !now.Before(entry.expires) {
			host.dropWorkspaceObservationLocked(id, entry)
		}
	}
}

func (host *WorkspaceFiles) evictOldestWorkspaceObservationLocked() bool {
	var oldest *workspacePageObservation
	for _, entry := range host.pageObservations {
		if oldest == nil || entry.lastUsed.Before(oldest.lastUsed) {
			oldest = entry
		}
	}
	if oldest == nil {
		return false
	}
	host.dropWorkspaceObservationLocked(oldest.id, oldest)
	return true
}

func (host *WorkspaceFiles) dropWorkspaceObservationLocked(id string, entry *workspacePageObservation) {
	delete(host.pageObservations, id)
	entry.cached = false
	if entry.active == 0 {
		host.pageMemoryBytes -= entry.charge
	}
}

func (host *WorkspaceFiles) releasePageObservation(entry *workspacePageObservation) {
	host.mu.Lock()
	entry.active--
	if entry.active == 0 && !entry.cached {
		host.pageMemoryBytes -= entry.charge
	}
	host.mu.Unlock()
}

func (host *WorkspaceFiles) releaseObservationScan() {
	host.mu.Lock()
	if host.pageScanning {
		host.pageScanning = false
		host.pageMemoryBytes -= workspaceObservationMaxBytes
	}
	host.cond.Broadcast()
	host.mu.Unlock()
}

func (host *WorkspaceFiles) reservePageScratch() error {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.closed {
		return fmt.Errorf("workspace reader closed")
	}
	if host.pageScratchBytes+workspaceObservationPageScratchBytes > workspacePageScratchLimit {
		return fmt.Errorf("workspace reader busy")
	}
	host.pageScratchBytes += workspaceObservationPageScratchBytes
	return nil
}

func (host *WorkspaceFiles) releasePageScratch() {
	host.mu.Lock()
	host.pageScratchBytes -= workspaceObservationPageScratchBytes
	host.mu.Unlock()
}
