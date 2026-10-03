package mcpstorage

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBrowserEntrypointsAndMentionContract(t *testing.T) {
	_, session := objectFixture(t, ObjectConfig{Browser: true})
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 7 {
		t.Fatalf("tools=%+v, err=%v", tools, err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "stow_browser" && tool.Name != "stow_object_mentions" {
			continue
		}
		requireReadOnlyBrowserTool(t, tool)
		wire, _ := json.Marshal(tool.Meta)
		if tool.Name == "stow_browser" && !strings.Contains(string(wire), `"entrypoints":[{"type":"global"},{"type":"thread"}]`) {
			t.Fatalf("entrypoint metadata: %s", wire)
		}
		if tool.Name == "stow_object_mentions" && !strings.Contains(string(wire), `"mentions/search":{}`) {
			t.Fatalf("mention metadata: %s", wire)
		}
	}
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "stow_object_mentions", Arguments: json.RawMessage(`{"query":""}`)})
	if err != nil || result.IsError || len(result.Content) != 0 {
		t.Fatalf("mention response=%+v, err=%v", result, err)
	}
	data, _ := json.Marshal(result.StructuredContent)
	if string(data) != `{"items":[]}` {
		t.Fatalf("empty mention result=%s", data)
	}
}

func requireReadOnlyBrowserTool(t *testing.T, tool *mcp.Tool) {
	t.Helper()
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
		t.Fatalf("unsafe annotations: %+v", tool)
	}
}

func TestBrowserPagesConfiguredBucketWithoutObservations(t *testing.T) {
	host, session := objectFixture(t, ObjectConfig{Browser: true})
	for n := 0; n < 51; n++ {
		_, err := host.runtime.PutObject(t.Context(), host.config.Bucket, fmt.Sprintf("notes/%02d", n), []byte("hello"), stow.PutOptions{ContentType: "text/plain"})
		if err != nil {
			t.Fatal(err)
		}
	}
	first := browserPageForTest(t, session, browserInput{Prefix: "notes/"})
	if len(first.Items) != 50 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	last := browserPageForTest(t, session, browserInput{Prefix: "notes/", Cursor: first.NextCursor})
	if len(last.Items) != 1 || last.Truncated || last.Items[0].Name != "notes/50" {
		t.Fatalf("last page=%+v", last)
	}
	if len(host.observations) != 0 {
		t.Fatal("browser consumed volatile save observations")
	}
	response, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "stow_browser", Arguments: json.RawMessage(`{"bucket":"outside"}`)})
	if err == nil && !response.IsError {
		t.Fatal("caller selected another bucket")
	}
}

func TestReadOnlyBrowserDoesNotWidenObjectAuthority(t *testing.T) {
	writer, _ := objectFixture(t, ObjectConfig{})
	dir := writer.config.Dir
	if _, err := writer.runtime.PutObject(t.Context(), "objects", "hello", []byte("hi"), stow.PutOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	host, session := objectFixture(t, ObjectConfig{Dir: dir, Bucket: "objects", Browser: true, ReadOnly: true})
	if got := browserPageForTest(t, session, browserInput{}); len(got.Items) != 1 {
		t.Fatalf("read-only page=%+v", got)
	}
	result := saveObjectForTest(t, session, objectSaveInput{Key: "hello", DataBase64: "bm8=", Replace: true, RequestKey: "attempt"})
	if result.Error == nil || result.Error.Code != "denied" {
		t.Fatalf("read-only save=%+v", result)
	}
	if len(host.observations) != 0 {
		t.Fatal("browser retained observations")
	}
}

func browserPageForTest(t *testing.T, session *mcp.ClientSession, input browserInput) browserPage {
	t.Helper()
	args, _ := json.Marshal(input)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "stow_browser", Arguments: json.RawMessage(args)})
	if err != nil || result.IsError {
		t.Fatalf("browser=%+v, err=%v", result, err)
	}
	data, _ := json.Marshal(result.StructuredContent)
	var page browserPage
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestBrowserResourcesAreScopedAndBounded(t *testing.T) {
	host, session := objectFixture(t, ObjectConfig{Browser: true, MaxObjectBytes: 10})
	for _, fixture := range []struct {
		key  string
		data []byte
	}{{"notes/☃?fragment#", []byte("hello")}, {"large", []byte("too large for preview")}} {
		if _, err := host.runtime.PutObject(t.Context(), host.config.Bucket, fixture.key, fixture.data, stow.PutOptions{ContentType: "text/plain"}); err != nil {
			t.Fatal(err)
		}
	}
	uri := host.objectURI("notes/☃?fragment#")
	resource, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: uri})
	if err != nil || len(resource.Contents) != 1 || resource.Contents[0].Text != "hello" {
		t.Fatalf("resource=%+v, err=%v", resource, err)
	}
	for _, invalid := range []string{host.objectURI("large"), host.objectURI("missing"), strings.Replace(uri, "/objects/", "/outside/", 1), uri + "?extra=1", "file:///etc/passwd"} {
		if _, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: invalid}); err == nil {
			t.Fatalf("read invalid resource %s", invalid)
		}
	}
	ui, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: browserURI})
	if err != nil || ui.Contents[0].MIMEType != browserMIME || !strings.Contains(ui.Contents[0].Text, "Stow bucket browser") {
		t.Fatalf("UI=%+v, err=%v", ui, err)
	}
	if len(host.observations) != 0 {
		t.Fatal("preview consumed a save observation")
	}
}

func TestBrowserEmptyAndBinaryResourceContent(t *testing.T) {
	host, session := objectFixture(t, ObjectConfig{Browser: true})
	for _, data := range [][]byte{{}, {0, 255}} {
		if _, err := host.runtime.PutObject(t.Context(), "objects", "data", data, stow.PutOptions{ContentType: "application/octet-stream"}); err != nil {
			t.Fatal(err)
		}
		result, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: host.objectURI("data")})
		if err != nil || len(result.Contents) != 1 {
			t.Fatalf("resource=%+v, err=%v", result, err)
		}
		wire, _ := json.Marshal(result.Contents[0])
		if !strings.Contains(string(wire), `"blob":`) {
			t.Fatalf("missing resource content: %s", wire)
		}
	}
}
