package mcpstorage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	workspaceFilesLimit    = 8
	workspaceResponseLimit = 256 << 10
	workspaceReadTimeout   = 10 * time.Second
)

// WorkspaceFiles owns a workspace reader for the lifetime of the MCP server.
type WorkspaceFiles struct {
	Server           *mcp.Server
	reader           *stow.WorkspaceReader
	checkpointReader *stow.CheckpointReader
	owner            *stow.Workspace
	editor           *stow.WorkspaceEditor
	workspaceID      string
	checkpointID     string
	mu               sync.Mutex
	cond             *sync.Cond
	closed           bool
	resourcesOnly    bool
	browserEnabled   bool
	active           int
	closeDone        chan struct{}
	timeout          time.Duration
	pageObservations map[string]*workspacePageObservation
	pageMemoryBytes  int64
	pageScratchBytes int64
	pageScanning     bool
	pageNow          func() time.Time
	readRoots        []string
	writeRoots       []string
	editing          bool
}

type workspaceOverview struct {
	Version               int      `json:"version"`
	WorkspaceID           string   `json:"workspace_id"`
	Bucket                string   `json:"bucket"`
	View                  string   `json:"view"`
	ReadOnly              bool     `json:"read_only"`
	Resources             []string `json:"resources"`
	ReadRoots             []string `json:"read_roots"`
	Features              []string `json:"features"`
	MaxFileBytes          int64    `json:"max_file_bytes"`
	MaxEntries            int      `json:"max_entries"`
	PagedRootURI          string   `json:"paged_root_uri"`
	MaxObservationEntries int      `json:"max_observation_entries"`
	MaxObservationBytes   int64    `json:"max_observation_bytes"`
	MaxObservations       int      `json:"max_observations"`
	MaxCacheBytes         int64    `json:"max_cache_bytes"`
	ObservationTTLSeconds int      `json:"observation_ttl_seconds"`
	WriteRoots            []string `json:"write_roots,omitempty"`
	Coordination          string   `json:"coordination,omitempty"`
}

type workspaceDirectory struct {
	Version     int              `json:"version"`
	WorkspaceID string           `json:"workspace_id"`
	Path        string           `json:"path"`
	View        string           `json:"view"`
	Entries     []workspaceEntry `json:"entries"`
	Complete    bool             `json:"complete"`
}

type workspaceEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
	URI      string `json:"uri"`
	RangeURI string `json:"range_uri,omitempty"`
}

func NewWorkspaceFiles(config Config, resourcesOnly bool) (*WorkspaceFiles, error) {
	if config.RegistryDir == "" || config.WorkspaceID == "" || config.MaxBytes <= 0 || config.MaxFiles <= 0 || config.Timeout <= 0 {
		return nil, fmt.Errorf("workspace resources require registry, workspace ID, positive bounds and timeout")
	}
	if resourcesOnly && (config.ExportRoot != "" || config.AdoptRoot != "") {
		return nil, fmt.Errorf("resource-only mode does not support transfer roots")
	}
	var server *mcp.Server
	var err error
	if resourcesOnly {
		server = mcp.NewServer(&mcp.Implementation{Name: "stow-workspace-files", Version: "1"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{Resources: &mcp.ResourceCapabilities{}}})
	} else {
		server, err = New(config)
		if err != nil {
			return nil, err
		}
	}
	host := &WorkspaceFiles{Server: server, workspaceID: config.WorkspaceID, checkpointID: config.CheckpointID, closeDone: make(chan struct{}), timeout: config.Timeout, resourcesOnly: resourcesOnly, pageObservations: make(map[string]*workspacePageObservation), pageNow: time.Now}
	if config.CheckpointID != "" {
		reader, err := stow.OpenCheckpointReader(context.Background(), stow.CheckpointReaderOptions{
			RegistryDir: config.RegistryDir, WorkspaceID: config.WorkspaceID, CheckpointID: config.CheckpointID, ReadRoots: config.ReadRoots,
		})
		if err != nil {
			return nil, fmt.Errorf("checkpoint resources unavailable")
		}
		host.checkpointReader = reader
		host.readRoots = append([]string(nil), reader.Reference().ReadRoots...)
	} else {
		reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: config.RegistryDir, WorkspaceID: config.WorkspaceID, ReadRoots: config.ReadRoots, MaxFileBytes: 65536, MaxEntries: 100})
		if err != nil {
			return nil, fmt.Errorf("workspace resources unavailable")
		}
		host.reader = reader
		host.readRoots = canonicalWorkspaceReadRoots(config.ReadRoots)
	}
	host.cond = sync.NewCond(&host.mu)
	server.AddReceivingMiddleware(host.workspaceResourceScopeMiddleware)
	if config.CheckpointID != "" {
		host.addCheckpointResources()
	} else {
		host.addWorkspaceResources()
	}
	return host, nil
}

func (host *WorkspaceFiles) workspaceResourceScopeMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		if method != "resources/read" {
			return next(ctx, method, request)
		}
		read, ok := request.(*mcp.ReadResourceRequest)
		if !ok {
			return next(ctx, method, request)
		}
		if read.Params.URI == "stow://guides/checkpoints/v1" {
			return next(ctx, method, request)
		}
		if read.Params.URI == workspaceBrowserURI {
			host.mu.Lock()
			enabled := host.browserEnabled && !host.closed
			host.mu.Unlock()
			if !enabled {
				return nil, workspaceNotFound()
			}
			return next(ctx, method, request)
		}
		if host.checkpointReader != nil {
			if _, err := host.parseCheckpointResourceURI(read.Params.URI); err != nil {
				return nil, workspaceNotFound()
			}
			return next(ctx, method, request)
		}
		if _, _, _, _, err := host.parseWorkspaceURI(read.Params.URI); err != nil {
			return nil, workspaceNotFound()
		}
		return next(ctx, method, request)
	}
}

func workspaceNotFound() error { return mcp.ResourceNotFoundError("") }

func canonicalWorkspaceReadRoots(configured []string) []string {
	if configured == nil {
		return []string{"."}
	}
	roots := append([]string(nil), configured...)
	sort.Strings(roots)
	unique := roots[:0]
	for _, root := range roots {
		if len(unique) == 0 || unique[len(unique)-1] != root {
			unique = append(unique, root)
		}
	}
	return unique
}

func (host *WorkspaceFiles) Close() error {
	host.mu.Lock()
	if host.closed {
		done := host.closeDone
		host.mu.Unlock()
		<-done
		return nil
	}
	host.closed = true
	for host.active != 0 {
		host.cond.Wait()
	}
	host.pageObservations = make(map[string]*workspacePageObservation)
	host.pageMemoryBytes = 0
	host.pageScratchBytes = 0
	host.pageScanning = false
	reader := host.reader
	checkpointReader := host.checkpointReader
	owner := host.owner
	editor := host.editor
	host.mu.Unlock()
	var err error
	if editor != nil {
		err = editor.Close()
	}
	if reader != nil {
		err = errors.Join(err, reader.Close())
	}
	if checkpointReader != nil {
		err = errors.Join(err, checkpointReader.Close())
	}
	if owner != nil {
		err = errors.Join(err, owner.Close())
	}
	close(host.closeDone)
	return err
}

func (host *WorkspaceFiles) begin(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc, error) {
	host.mu.Lock()
	if host.closed {
		host.mu.Unlock()
		return nil, nil, fmt.Errorf("workspace reader closed")
	}
	if host.active >= workspaceFilesLimit {
		host.mu.Unlock()
		return nil, nil, fmt.Errorf("workspace reader busy")
	}
	host.active++
	host.mu.Unlock()
	d := timeout
	if d > host.timeout {
		d = host.timeout
	}
	if d > workspaceReadTimeout {
		d = workspaceReadTimeout
	}
	child, cancel := context.WithTimeout(ctx, d)
	return child, func() { cancel(); host.mu.Lock(); host.active--; host.cond.Broadcast(); host.mu.Unlock() }, nil
}

func (host *WorkspaceFiles) addWorkspaceResources() {
	id := host.workspaceID
	root := "stow://workspaces/" + id + "/v1"
	host.Server.AddResource(&mcp.Resource{URI: root, Name: "Workspace overview", MIMEType: "application/json"}, host.readWorkspaceResource)
	host.Server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "stow://workspaces/" + id + "/tree/{path}", Name: "Workspace directory", MIMEType: "application/json"}, host.readWorkspaceResource)
	host.Server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "stow://workspaces/" + id + "/tree-pages/v1/{path}", Name: "Workspace directory page", MIMEType: "application/json"}, host.readWorkspaceResource)
	host.Server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "stow://workspaces/" + id + "/tree-pages/v1/{path}/{cursor}", Name: "Workspace directory page", MIMEType: "application/json"}, host.readWorkspaceResource)
	host.Server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "stow://workspaces/" + id + "/files/{path}", Name: "Workspace file"}, host.readWorkspaceResource)
}

func (host *WorkspaceFiles) resourceWorkspaceID() string {
	if host.workspaceID != "" {
		return host.workspaceID
	}
	if host.reader != nil {
		return host.reader.Reference().ID
	}
	return ""
}

func workspacePathURI(kind, id, name string) string {
	return "stow://workspaces/" + id + "/" + kind + "/" + base64.RawURLEncoding.EncodeToString([]byte(name))
}

func (host *WorkspaceFiles) parseWorkspaceURI(uri string) (kind, id, name string, cursor *workspacePageCursor, err error) {
	if host.isWorkspacePageRoute(uri) {
		name, cursor, err = host.parseWorkspacePageURI(uri)
		if err != nil {
			return "", "", "", nil, err
		}
		return "tree-pages", host.resourceWorkspaceID(), name, cursor, nil
	}
	if len(uri) > 2048 || strings.ContainsAny(uri, "?#") || strings.Contains(uri, "\\") {
		return "", "", "", nil, workspaceNotFound()
	}
	prefix := "stow://workspaces/"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", "", nil, workspaceNotFound()
	}
	parts := strings.Split(strings.TrimPrefix(uri, prefix), "/")
	kind, id, name, err = parseWorkspaceParts(uri, parts, host.resourceWorkspaceID())
	return kind, id, name, nil, err
}

func parseWorkspaceParts(uri string, parts []string, allowedID string) (kind, id, name string, err error) {
	if len(parts) == 2 && parts[0] == allowedID && parts[1] == "v1" {
		return "overview", allowedID, ".", nil
	}
	if len(parts) != 3 || parts[0] != allowedID {
		return "", "", "", workspaceNotFound()
	}
	kind = parts[1]
	if kind != "tree" && kind != "files" {
		return "", "", "", workspaceNotFound()
	}
	decoded, e := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	name = string(decoded)
	if e != nil || workspacePathURI(kind, allowedID, name) != uri || !validWorkspaceURIPath(decoded, name, kind) {
		return "", "", "", workspaceNotFound()
	}
	return kind, allowedID, name, nil
}

func validWorkspaceURIPath(decoded []byte, name, kind string) bool {
	if len(decoded) == 0 || len(decoded) > 1024 || !utf8.Valid(decoded) {
		return false
	}
	if name == "." {
		return kind == "tree"
	}
	return canonicalWorkspacePath(name)
}

func canonicalWorkspacePath(name string) bool {
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	for _, r := range name {
		if r == 0 || r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func (host *WorkspaceFiles) readWorkspaceResource(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	kind, id, name, cursor, err := host.parseWorkspaceURI(request.Params.URI)
	if err != nil {
		return nil, workspaceNotFound()
	}
	ctx, done, err := host.begin(ctx, host.timeout)
	if err != nil {
		return nil, err
	}
	defer done()
	content, err := host.workspaceResourceContent(ctx, workspaceResourceRequest{uri: request.Params.URI, kind: kind, id: id, name: name, cursor: cursor})
	if err != nil {
		return nil, err
	}
	result := &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{content}}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > workspaceResponseLimit {
		return nil, fmt.Errorf("workspace resource exceeds response limit")
	}
	return result, nil
}

type workspaceResourceRequest struct {
	uri    string
	kind   string
	id     string
	name   string
	cursor *workspacePageCursor
}

func (host *WorkspaceFiles) workspaceResourceContent(ctx context.Context, request workspaceResourceRequest) (*mcp.ResourceContents, error) {
	switch request.kind {
	case "overview":
		return host.workspaceOverviewContent(request.uri, request.id), nil
	case "tree":
		return host.workspaceDirectoryContent(ctx, request.uri, request.id, request.name)
	case "files":
		return host.workspaceFileContent(ctx, request.uri, request.name)
	case "tree-pages":
		return host.workspacePageContent(ctx, request.uri, request.id, request.name, request.cursor)
	}
	return nil, fmt.Errorf("workspace resource unavailable")
}

func (host *WorkspaceFiles) workspaceOverviewContent(uri, id string) *mcp.ResourceContents {
	ref := host.reader.Reference()
	pagedRoot := workspacePageURI(id, ".")
	features := []string{"directory", "file", "paged_directory", "scoped_read_roots"}
	coordination := ""
	if host.editing {
		features = append(features, "workspace_file_editing")
		coordination = "host_quiesced"
	}
	overview := workspaceOverview{Version: 1, WorkspaceID: id, Bucket: ref.Bucket, View: "mutable working view", ReadOnly: !host.editing, Resources: []string{workspacePathURI("tree", id, "."), "stow://workspaces/" + id + "/tree/{path}", "stow://workspaces/" + id + "/files/{path}", pagedRoot}, ReadRoots: append([]string{}, host.readRoots...), Features: features, MaxFileBytes: 65536, MaxEntries: 100, PagedRootURI: pagedRoot, MaxObservationEntries: workspaceObservationMaxEntries, MaxObservationBytes: workspaceObservationMaxBytes, MaxObservations: workspaceObservationMaxCount, MaxCacheBytes: workspaceObservationMaxCacheBytes, ObservationTTLSeconds: int(workspaceObservationTTL.Seconds()), WriteRoots: append([]string(nil), host.writeRoots...), Coordination: coordination}
	body, _ := json.Marshal(overview)
	return &mcp.ResourceContents{URI: uri, MIMEType: "application/json", Text: string(body)}
}

func (host *WorkspaceFiles) workspaceDirectoryContent(ctx context.Context, uri, id, name string) (*mcp.ResourceContents, error) {
	entries, err := host.reader.ReadDirectory(ctx, name)
	if err != nil {
		return nil, workspaceReadError(err)
	}
	result := workspaceDirectory{Version: 1, WorkspaceID: id, Path: name, View: "mutable working view", Entries: make([]workspaceEntry, 0, len(entries)), Complete: true}
	for _, entry := range entries {
		kind := "files"
		if entry.Kind == "directory" {
			kind = "tree"
		}
		result.Entries = append(result.Entries, workspaceEntry{Name: entry.Name, Path: entry.Path, Kind: entry.Kind, Size: entry.Size, URI: workspacePathURI(kind, id, entry.Path)})
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("workspace resource unavailable")
	}
	return &mcp.ResourceContents{URI: uri, MIMEType: "application/json", Text: string(body)}, nil
}

func (host *WorkspaceFiles) workspaceFileContent(ctx context.Context, uri, name string) (*mcp.ResourceContents, error) {
	file, err := host.reader.ReadFile(ctx, name)
	if err != nil {
		return nil, workspaceReadError(err)
	}
	mimeType := file.ContentType
	if mimeType == "" {
		mimeType = mime.TypeByExtension(path.Ext(name))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	content := &mcp.ResourceContents{URI: uri, MIMEType: mimeType}
	if textWorkspaceMedia(mimeType) && utf8.Valid(file.Data) {
		content.Text = string(file.Data)
	} else {
		content.Blob = file.Data
	}
	if len(file.Data) == 0 {
		content.Blob = []byte{}
	}
	return content, nil
}

func textWorkspaceMedia(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/") || mimeType == "application/json" || strings.HasSuffix(mimeType, "+json") || strings.HasSuffix(mimeType, "+xml")
}

func workspaceReadError(err error) error {
	switch {
	case errors.Is(err, stow.ErrWorkspaceReaderClosed):
		return fmt.Errorf("workspace reader closed")
	case errors.Is(err, stow.ErrWorkspaceReadTooLarge):
		return fmt.Errorf("workspace resource exceeds read bounds")
	case errors.Is(err, stow.ErrWorkspaceReadChanged):
		return fmt.Errorf("workspace resource changed during read")
	case errors.Is(err, stow.ErrWorkspaceReadUnavailable):
		return workspaceNotFound()
	default:
		return workspaceNotFound()
	}
}
