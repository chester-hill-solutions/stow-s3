package main

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPBrowserNativeReadOnlyStdio(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("native object host requires Linux or macOS")
	}
	dir := t.TempDir()
	prepareBrowserStore(t, dir)
	process := startObjectMCPProcess(t, dir, []string{"--browser", "--read-only"}, "2026-07-28")
	uri := browserResourceURIForTest(t, process)
	object, err := process.session.ReadResource(process.context, &mcp.ReadResourceParams{URI: uri})
	if err != nil || object.Contents[0].Text != "hello" {
		t.Fatalf("object=%+v, err=%v", object, err)
	}
	ui, err := process.session.ReadResource(process.context, &mcp.ReadResourceParams{URI: "ui://stow/browser-v1"})
	if err != nil || !strings.Contains(ui.Contents[0].Text, "Stow bucket browser") {
		t.Fatalf("UI resource error=%v", err)
	}
	args := objectSaveArguments("changed", "read-only-attempt")
	args["replace"] = true
	refused := callObjectMCP(t, process, "stow_object_save", args, true)
	if refused.Error == nil || refused.Error.Code != "denied" {
		t.Fatalf("read-only save=%+v", refused)
	}
	closeObjectMCPProcess(t, process)
}

func browserResourceURIForTest(t *testing.T, process *objectMCPProcess) string {
	t.Helper()
	listed, err := process.session.CallTool(process.context, &mcp.CallToolParams{Name: "stow_browser", Arguments: json.RawMessage(`{}`)})
	if err != nil || listed.IsError {
		t.Fatalf("browser=%+v, err=%v", listed, err)
	}
	var page struct {
		Items []struct {
			URI string `json:"uri"`
		} `json:"items"`
	}
	data, _ := json.Marshal(listed.StructuredContent)
	if err := json.Unmarshal(data, &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("page=%s, err=%v", data, err)
	}
	return page.Items[0].URI
}

func prepareBrowserStore(t *testing.T, dir string) {
	t.Helper()
	store, err := stow.OpenFilesystem(stow.FilesystemOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateBucket(t.Context(), "objects"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(t.Context(), "objects", "report", []byte("hello"), stow.PutOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
}
