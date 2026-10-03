package mcpstorage

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const browserURI = "ui://stow/browser-v1"
const browserMIME = "text/html;profile=mcp-app"
const browserPageSize = 50

//go:embed browser.html.gz
var browserCompressed []byte

type browserInput struct {
	Prefix string `json:"prefix,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}
type mentionInput struct {
	Query string `json:"query"`
}
type browserItem struct {
	Type     string `json:"type"`
	URI      string `json:"uri"`
	Name     string `json:"name"`
	MIMEType string `json:"mimeType"`
	Size     int64  `json:"size"`
}
type browserPage struct {
	Bucket     string        `json:"bucket"`
	Items      []browserItem `json:"items"`
	Truncated  bool          `json:"truncated"`
	NextCursor string        `json:"next_cursor,omitempty"`
	MaxBytes   int           `json:"max_bytes"`
}
type mentionResult struct {
	Items []browserItem `json:"items"`
}

func (host *ObjectServer) addBrowser() {
	closedWorld := false
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closedWorld}
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_browser", Title: "Stow bucket browser", Description: "Browse a bounded page in the configured bucket by key prefix. This browser never saves or deletes objects.", Annotations: annotations,
		Meta: mcp.Meta{"ui": browserToolUI{ResourceURI: browserURI}, "openai/ui": browserEntrypoints{Entrypoints: []browserEntry{{Type: "global"}, {Type: "thread"}}}}}, host.browse)
	mcp.AddTool(host.Server, &mcp.Tool{Name: "stow_object_mentions", Description: "Find up to 50 object references by key prefix in the configured bucket.", Annotations: annotations,
		Meta: mcp.Meta{"ui": browserToolUI{Visibility: []string{"app"}}, "openai/extensions": browserMentions{}}}, host.mentions)
	host.Server.AddResource(&mcp.Resource{URI: browserURI, Name: "Stow bucket browser", MIMEType: browserMIME}, readBrowserUI)
	host.Server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "stow://objects/{bucket}/{key}", Name: "Configured bucket object", Description: "Bounded current object contents; the key is an opaque base64url identifier."}, host.readBrowserResource)
}

func (host *ObjectServer) browserPage(ctx context.Context, input browserInput) (browserPage, error) {
	if len(input.Prefix) > 1024 || len(input.Cursor) > 4096 {
		return browserPage{}, fmt.Errorf("browser input exceeds bounds")
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.beforeOperation(ctx); err != nil {
		return browserPage{}, fmt.Errorf("object browser unavailable")
	}
	page, err := host.runtime.ListObjects(ctx, host.config.Bucket, stow.ListOptions{Prefix: input.Prefix, Cursor: input.Cursor, Limit: browserPageSize})
	if err != nil {
		return browserPage{}, fmt.Errorf("object listing unavailable")
	}
	result := browserPage{Bucket: host.config.Bucket, Items: make([]browserItem, 0, len(page.Objects)), Truncated: page.Truncated, NextCursor: page.NextCursor, MaxBytes: host.config.MaxObjectBytes}
	for _, object := range page.Objects {
		mime := object.ContentType
		if mime == "" {
			mime = "application/octet-stream"
		}
		result.Items = append(result.Items, browserItem{Type: "resource_link", URI: host.objectURI(object.Key), Name: object.Key, MIMEType: mime, Size: object.Size})
	}
	return result, nil
}

func (host *ObjectServer) browse(ctx context.Context, _ *mcp.CallToolRequest, input browserInput) (*mcp.CallToolResult, browserPage, error) {
	page, err := host.browserPage(ctx, input)
	return &mcp.CallToolResult{Content: []mcp.Content{}}, page, err
}

func (host *ObjectServer) mentions(ctx context.Context, _ *mcp.CallToolRequest, input mentionInput) (*mcp.CallToolResult, mentionResult, error) {
	page, err := host.browserPage(ctx, browserInput{Prefix: input.Query})
	return &mcp.CallToolResult{Content: []mcp.Content{}}, mentionResult{Items: page.Items}, err
}

func (host *ObjectServer) objectURI(key string) string {
	return "stow://objects/" + host.config.Bucket + "/" + base64.RawURLEncoding.EncodeToString([]byte(key))
}

func (host *ObjectServer) resourceKey(uri string) (string, error) {
	prefix := "stow://objects/" + host.config.Bucket + "/"
	if !strings.HasPrefix(uri, prefix) || len(uri) > 2048 {
		return "", fmt.Errorf("object resource is outside the configured scope")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(uri, prefix))
	key := string(decoded)
	if err != nil || storage.ValidateKey(key) != nil || host.objectURI(key) != uri {
		return "", fmt.Errorf("invalid object resource")
	}
	return key, nil
}

func (host *ObjectServer) readBrowserResource(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	key, err := host.resourceKey(request.Params.URI)
	if err != nil {
		return nil, err
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.beforeOperation(ctx); err != nil {
		return nil, fmt.Errorf("object resource unavailable")
	}
	if err := host.readPreflight(ctx, key); err != nil {
		return nil, fmt.Errorf("object resource unavailable or exceeds preview bounds")
	}
	object, err := host.runtime.GetObject(ctx, host.config.Bucket, key)
	if err != nil || len(object.Data) > host.config.MaxObjectBytes {
		return nil, fmt.Errorf("object resource unavailable or exceeds preview bounds")
	}
	content := &mcp.ResourceContents{URI: request.Params.URI, MIMEType: object.ContentType}
	if content.MIMEType == "" {
		content.MIMEType = "application/octet-stream"
	}
	if utf8.Valid(object.Data) && (strings.HasPrefix(content.MIMEType, "text/") || content.MIMEType == "application/json") {
		content.Text = string(object.Data)
	} else {
		content.Blob = object.Data
	}
	if len(object.Data) == 0 {
		content.Blob = []byte{}
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{content}}, nil
}
