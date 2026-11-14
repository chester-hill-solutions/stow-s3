package mcpstorage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	checkpointResourceMaxEntries  = 100
	checkpointResourceMaxBytes    = 65536
	checkpointResourceMaxVerify   = 64 << 20
	checkpointResourceMaxFiles    = 10000
	checkpointResourceMaxMetadata = 4 << 20
)

type checkpointResourceRequest struct {
	kind   string
	path   string
	offset int64
	length int64
}

type checkpointResourceDTO interface {
	checkpointResourceOverview | checkpointResourcePage | checkpointRangeContent
}

type checkpointResourceOverview struct {
	Version            int      `json:"version"`
	WorkspaceID        string   `json:"workspace_id"`
	CheckpointID       string   `json:"checkpoint_id"`
	View               string   `json:"view"`
	ManifestSHA256     string   `json:"manifest_sha256"`
	CreatedAt          string   `json:"created_at"`
	ExpiresAt          string   `json:"expires_at"`
	RootURI            string   `json:"root_uri"`
	ReadRoots          []string `json:"read_roots"`
	FilteredView       string   `json:"filtered_view"`
	MaxEntries         int      `json:"max_entries"`
	MaxFileBytes       int64    `json:"max_file_bytes"`
	MaxRangeBytes      int64    `json:"max_range_bytes"`
	MaxVerifyFileBytes int64    `json:"max_verify_file_bytes"`
	MaxManifestFiles   int      `json:"max_manifest_files"`
	MaxMetadataBytes   int64    `json:"max_metadata_bytes"`
}

type checkpointResourcePage struct {
	Version        int                    `json:"version"`
	WorkspaceID    string                 `json:"workspace_id"`
	CheckpointID   string                 `json:"checkpoint_id"`
	View           string                 `json:"view"`
	ManifestSHA256 string                 `json:"manifest_sha256"`
	CreatedAt      string                 `json:"created_at"`
	ExpiresAt      string                 `json:"expires_at"`
	Path           string                 `json:"path"`
	Entries        []workspaceEntry       `json:"entries"`
	Page           checkpointPageMetadata `json:"page"`
}

type checkpointPageMetadata struct {
	Offset  int    `json:"offset"`
	Count   int    `json:"count"`
	NextURI string `json:"next_uri,omitempty"`
}

type checkpointRangeContent struct {
	Version        int     `json:"version"`
	WorkspaceID    string  `json:"workspace_id"`
	CheckpointID   string  `json:"checkpoint_id"`
	View           string  `json:"view"`
	ManifestSHA256 string  `json:"manifest_sha256"`
	CreatedAt      string  `json:"created_at"`
	ExpiresAt      string  `json:"expires_at"`
	Path           string  `json:"path"`
	Size           int64   `json:"size"`
	SHA256         string  `json:"sha256"`
	Offset         int64   `json:"offset"`
	Length         int     `json:"length"`
	Text           *string `json:"text,omitempty"`
	BlobBase64     *string `json:"blob_base64,omitempty"`
	NextURI        string  `json:"next_uri,omitempty"`
}

func (host *WorkspaceFiles) addCheckpointResources() {
	base := "stow://workspaces/" + host.workspaceID + "/checkpoints/" + host.checkpointID
	overview := base + "/v1"
	host.Server.AddResource(&mcp.Resource{URI: overview, Name: "Checkpoint overview", MIMEType: "application/json"}, host.readCheckpointResource)
	host.Server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: base + "/tree/v1/{path}", Name: "Checkpoint directory", MIMEType: "application/json"}, host.readCheckpointResource)
	host.Server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: base + "/tree/v1/{path}/{offset}", Name: "Checkpoint directory page", MIMEType: "application/json"}, host.readCheckpointResource)
	host.Server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: base + "/files/{path}", Name: "Checkpoint file"}, host.readCheckpointResource)
	host.Server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: base + "/ranges/v1/{path}/{offset}/{length}", Name: "Checkpoint file range", MIMEType: "application/json"}, host.readCheckpointResource)
}

func (host *WorkspaceFiles) parseCheckpointResourceURI(uri string) (checkpointResourceRequest, error) {
	if len(uri) > 2048 || strings.ContainsAny(uri, "?#\\") {
		return checkpointResourceRequest{}, workspaceNotFound()
	}
	base := "stow://workspaces/" + host.workspaceID + "/checkpoints/" + host.checkpointID + "/"
	if !strings.HasPrefix(uri, base) {
		return checkpointResourceRequest{}, workspaceNotFound()
	}
	return parseCheckpointResourceRoute(strings.TrimPrefix(uri, base))
}

func parseCheckpointResourceRoute(route string) (checkpointResourceRequest, error) {
	if route == "v1" {
		return checkpointResourceRequest{kind: "overview"}, nil
	}
	parts := strings.Split(route, "/")
	if len(parts) == 0 {
		return checkpointResourceRequest{}, workspaceNotFound()
	}
	switch parts[0] {
	case "tree":
		return parseCheckpointTreeRoute(parts)
	case "files":
		return parseCheckpointFileRoute(parts)
	case "ranges":
		return parseCheckpointRangeRoute(parts)
	}
	return checkpointResourceRequest{}, workspaceNotFound()
}

func parseCheckpointTreeRoute(parts []string) (checkpointResourceRequest, error) {
	if (len(parts) != 3 && len(parts) != 4) || parts[1] != "v1" {
		return checkpointResourceRequest{}, workspaceNotFound()
	}
	name, ok := decodeCheckpointURIPath(parts[2], true)
	if !ok {
		return checkpointResourceRequest{}, workspaceNotFound()
	}
	request := checkpointResourceRequest{kind: "tree", path: name}
	if len(parts) == 4 {
		request.offset, ok = canonicalCheckpointDecimal(parts[3])
		if !ok || request.offset > int64(^uint(0)>>1) {
			return checkpointResourceRequest{}, workspaceNotFound()
		}
	}
	return request, nil
}

func parseCheckpointFileRoute(parts []string) (checkpointResourceRequest, error) {
	if len(parts) != 2 {
		return checkpointResourceRequest{}, workspaceNotFound()
	}
	name, ok := decodeCheckpointURIPath(parts[1], false)
	if !ok {
		return checkpointResourceRequest{}, workspaceNotFound()
	}
	return checkpointResourceRequest{kind: "files", path: name}, nil
}

func parseCheckpointRangeRoute(parts []string) (checkpointResourceRequest, error) {
	if len(parts) != 5 || parts[1] != "v1" {
		return checkpointResourceRequest{}, workspaceNotFound()
	}
	name, ok := decodeCheckpointURIPath(parts[2], false)
	if !ok {
		return checkpointResourceRequest{}, workspaceNotFound()
	}
	offset, offsetOK := canonicalCheckpointDecimal(parts[3])
	length, lengthOK := canonicalCheckpointDecimal(parts[4])
	if !offsetOK || !lengthOK || length < 1 || length > checkpointResourceMaxBytes {
		return checkpointResourceRequest{}, workspaceNotFound()
	}
	return checkpointResourceRequest{kind: "ranges", path: name, offset: offset, length: length}, nil
}

func decodeCheckpointURIPath(encoded string, allowRoot bool) (string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != encoded || !utf8.Valid(decoded) || len(decoded) == 0 || len(decoded) > 1024 {
		return "", false
	}
	name := string(decoded)
	if name == "." {
		return name, allowRoot
	}
	return name, canonicalWorkspacePath(name)
}

func canonicalCheckpointDecimal(value string) (int64, bool) {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil && parsed >= 0 && strconv.FormatInt(parsed, 10) == value
}

func (host *WorkspaceFiles) readCheckpointResource(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	parsed, err := host.parseCheckpointResourceURI(request.Params.URI)
	if err != nil {
		return nil, workspaceNotFound()
	}
	readCtx, done, err := host.begin(ctx, host.timeout)
	if err != nil {
		return nil, err
	}
	defer done()
	var content *mcp.ResourceContents
	switch parsed.kind {
	case "overview":
		content, err = host.checkpointOverviewContent(readCtx, request.Params.URI)
	case "tree":
		content, err = host.checkpointDirectoryContent(readCtx, request.Params.URI, parsed)
	case "files":
		content, err = host.checkpointFileContent(readCtx, request.Params.URI, parsed.path)
	case "ranges":
		content, err = host.checkpointRangeContent(readCtx, request.Params.URI, parsed)
	}
	if err != nil {
		return nil, checkpointResourceError(err)
	}
	result := &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{content}}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > workspaceResponseLimit {
		return nil, fmt.Errorf("checkpoint resource exceeds response limit")
	}
	return result, nil
}

func (host *WorkspaceFiles) checkpointOverviewContent(ctx context.Context, uri string) (*mcp.ResourceContents, error) {
	if _, err := host.checkpointReader.ReadDirectory(ctx, ".", 0, 1); err != nil {
		return nil, err
	}
	ref := host.checkpointReader.Reference()
	root := checkpointTreeURI(ref, ".", 0, false)
	value := checkpointResourceOverview{
		Version: 1, WorkspaceID: ref.WorkspaceID, CheckpointID: ref.CheckpointID, View: "checkpoint files",
		ManifestSHA256: ref.ManifestSHA256, CreatedAt: ref.CreatedAt.Format(time.RFC3339Nano), ExpiresAt: ref.ExpiresAt.Format(time.RFC3339Nano),
		RootURI: root, ReadRoots: append([]string{}, ref.ReadRoots...), FilteredView: "manifest files within read roots after mandatory exclusions", MaxEntries: checkpointResourceMaxEntries,
		MaxFileBytes: checkpointResourceMaxBytes, MaxRangeBytes: checkpointResourceMaxBytes, MaxVerifyFileBytes: checkpointResourceMaxVerify,
		MaxManifestFiles: checkpointResourceMaxFiles, MaxMetadataBytes: checkpointResourceMaxMetadata,
	}
	return jsonResource(uri, value)
}

func (host *WorkspaceFiles) checkpointDirectoryContent(ctx context.Context, uri string, request checkpointResourceRequest) (*mcp.ResourceContents, error) {
	ref := host.checkpointReader.Reference()
	limit := checkpointResourceMaxEntries
	for {
		page, err := host.checkpointReader.ReadDirectory(ctx, request.path, int(request.offset), limit)
		if err != nil {
			return nil, err
		}
		content, err := buildCheckpointDirectoryPage(uri, request.path, page, ref)
		if err != nil {
			return nil, err
		}
		if encodedResourceSize(content) <= workspaceResponseLimit {
			return content, nil
		}
		if limit == 1 {
			return nil, fmt.Errorf("checkpoint resource exceeds response limit")
		}
		limit = max(1, limit/2)
	}
}

func buildCheckpointDirectoryPage(uri, directory string, page stow.CheckpointReadDirectory, ref stow.CheckpointReadReference) (*mcp.ResourceContents, error) {
	entries := make([]workspaceEntry, 0, len(page.Entries))
	for _, entry := range page.Entries {
		kind := "files"
		if entry.Kind == "directory" {
			kind = "tree"
		}
		item := workspaceEntry{Name: entry.Name, Path: entry.Path, Kind: entry.Kind, Size: entry.Size, URI: checkpointPathURI(ref, kind, entry.Path)}
		if entry.Kind == "file" {
			item.RangeURI = checkpointRangeURI(ref, entry.Path, 0, checkpointResourceMaxBytes)
		}
		entries = append(entries, item)
	}
	next := ""
	if page.Offset+len(page.Entries) < page.Total {
		if len(page.Entries) == 0 {
			return nil, workspaceNotFound()
		}
		next = checkpointTreeURI(ref, directory, page.Offset+len(page.Entries), true)
	}
	value := checkpointResourcePage{
		Version: 1, WorkspaceID: ref.WorkspaceID, CheckpointID: ref.CheckpointID, View: "checkpoint files",
		ManifestSHA256: ref.ManifestSHA256, CreatedAt: ref.CreatedAt.Format(time.RFC3339Nano), ExpiresAt: ref.ExpiresAt.Format(time.RFC3339Nano),
		Path: directory, Entries: entries, Page: checkpointPageMetadata{Offset: page.Offset, Count: len(entries), NextURI: next},
	}
	return jsonResource(uri, value)
}

func (host *WorkspaceFiles) checkpointFileContent(ctx context.Context, uri, name string) (*mcp.ResourceContents, error) {
	result, err := host.checkpointReader.ReadFile(ctx, name)
	if err != nil {
		return nil, err
	}
	content := &mcp.ResourceContents{URI: uri, MIMEType: checkpointMIME(name)}
	if textWorkspaceMedia(content.MIMEType) && utf8.Valid(result.Data) {
		content.Text = string(result.Data)
	} else {
		content.Blob = result.Data
	}
	if len(result.Data) == 0 {
		content.Blob = []byte{}
	}
	return content, nil
}

func (host *WorkspaceFiles) checkpointRangeContent(ctx context.Context, uri string, request checkpointResourceRequest) (*mcp.ResourceContents, error) {
	result, err := host.checkpointReader.ReadRange(ctx, request.path, request.offset, request.length)
	if err != nil {
		return nil, err
	}
	ref := host.checkpointReader.Reference()
	value := checkpointRangeContent{
		Version: 1, WorkspaceID: ref.WorkspaceID, CheckpointID: ref.CheckpointID, View: "checkpoint files",
		ManifestSHA256: ref.ManifestSHA256, CreatedAt: ref.CreatedAt.Format(time.RFC3339Nano), ExpiresAt: ref.ExpiresAt.Format(time.RFC3339Nano),
		Path: result.Path, Size: result.Size, SHA256: result.SHA256, Offset: result.Offset, Length: len(result.Data),
	}
	if textWorkspaceMedia(checkpointMIME(request.path)) && utf8.Valid(result.Data) {
		text := string(result.Data)
		value.Text = &text
	} else {
		encoded := base64.StdEncoding.EncodeToString(result.Data)
		value.BlobBase64 = &encoded
	}
	if result.Offset+int64(len(result.Data)) < result.Size {
		value.NextURI = checkpointRangeURI(ref, request.path, result.Offset+int64(len(result.Data)), request.length)
	}
	content, err := jsonResource(uri, value)
	if err != nil || encodedResourceSize(content) > workspaceResponseLimit {
		if value.Text == nil {
			return nil, fmt.Errorf("checkpoint resource exceeds response limit")
		}
		value.Text = nil
		encoded := base64.StdEncoding.EncodeToString(result.Data)
		value.BlobBase64 = &encoded
		content, err = jsonResource(uri, value)
	}
	return content, err
}

func checkpointMIME(name string) string {
	if value := mime.TypeByExtension(path.Ext(name)); value != "" {
		return value
	}
	return "application/octet-stream"
}

func checkpointPathURI(ref stow.CheckpointReadReference, kind, name string) string {
	base := "stow://workspaces/" + ref.WorkspaceID + "/checkpoints/" + ref.CheckpointID + "/"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(name))
	if kind == "tree" {
		return base + "tree/v1/" + encoded
	}
	return base + "files/" + encoded
}

func checkpointTreeURI(ref stow.CheckpointReadReference, name string, offset int, continuation bool) string {
	uri := "stow://workspaces/" + ref.WorkspaceID + "/checkpoints/" + ref.CheckpointID + "/tree/v1/" + base64.RawURLEncoding.EncodeToString([]byte(name))
	if continuation {
		uri += "/" + strconv.Itoa(offset)
	}
	return uri
}

func checkpointRangeURI(ref stow.CheckpointReadReference, name string, offset, length int64) string {
	return "stow://workspaces/" + ref.WorkspaceID + "/checkpoints/" + ref.CheckpointID + "/ranges/v1/" +
		base64.RawURLEncoding.EncodeToString([]byte(name)) + "/" + strconv.FormatInt(offset, 10) + "/" + strconv.FormatInt(length, 10)
}

func jsonResource[T checkpointResourceDTO](uri string, value T) (*mcp.ResourceContents, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("checkpoint resource unavailable")
	}
	return &mcp.ResourceContents{URI: uri, MIMEType: "application/json", Text: string(body)}, nil
}

func encodedResourceSize(content *mcp.ResourceContents) int {
	encoded, err := json.Marshal(&mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{content}})
	if err != nil {
		return workspaceResponseLimit + 1
	}
	return len(encoded)
}

func checkpointResourceError(err error) error {
	switch {
	case errors.Is(err, stow.ErrCheckpointReadExpired):
		return fmt.Errorf("checkpoint reader expired")
	case errors.Is(err, stow.ErrCheckpointReadClosed):
		return fmt.Errorf("checkpoint reader closed")
	case errors.Is(err, stow.ErrCheckpointReadTooLarge):
		return fmt.Errorf("checkpoint resource exceeds read bounds")
	case errors.Is(err, stow.ErrCheckpointReadUnavailable):
		return workspaceNotFound()
	default:
		return workspaceNotFound()
	}
}
