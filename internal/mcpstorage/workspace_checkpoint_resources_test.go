package mcpstorage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCheckpointWorkspaceResourcesReadImmutableRangeAndScope(t *testing.T) {
	fixture := newCheckpointResourceFixture(t)
	assertResourceOnly(t, fixture.session)
	largeURI, emptyURI := checkpointPageFileURIs(t, fixture)
	assertCheckpointRangeBytes(t, fixture, largeURI, emptyURI)
	assertCheckpointScopeRefusals(t, fixture)
}

type checkpointResourceFixture struct {
	workspace   *stow.Workspace
	checkpoint  stow.CheckpointInfo
	host        *WorkspaceFiles
	session     *mcp.ClientSession
	payload     []byte
	overviewURI string
}

func newCheckpointResourceFixture(t *testing.T) checkpointResourceFixture {
	t.Helper()
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	if err := os.Mkdir(filepath.Join(ws.Dir(), "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 90_000)
	for index := range payload {
		payload[index] = byte(index * 17)
	}
	writeCheckpointTestFile(t, filepath.Join(ws.Dir(), "docs", "large.bin"), payload)
	writeCheckpointTestFile(t, filepath.Join(ws.Dir(), "docs", "empty.txt"), nil)
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatal(err)
	}
	writeCheckpointTestFile(t, filepath.Join(ws.Dir(), "docs", "large.bin"), []byte("current bytes"))
	host, err := NewWorkspaceFiles(Config{RegistryDir: registry, WorkspaceID: ws.ID(), CheckpointID: checkpoint.ID,
		MaxBytes: 1 << 20, MaxFiles: 100, Timeout: 5 * time.Second, ReadRoots: []string{"docs"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	session, closeSessions := workspaceResourceSession(t, host)
	t.Cleanup(closeSessions)
	return checkpointResourceFixture{workspace: ws, checkpoint: checkpoint, host: host, session: session, payload: payload,
		overviewURI: checkpointOverviewURI(ws.ID(), checkpoint.ID)}
}

func checkpointPageFileURIs(t *testing.T, fixture checkpointResourceFixture) (string, string) {
	t.Helper()
	overviewResult, err := fixture.session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: fixture.overviewURI})
	if err != nil {
		t.Fatal(err)
	}
	var overview checkpointResourceOverview
	if err := json.Unmarshal([]byte(overviewResult.Contents[0].Text), &overview); err != nil {
		t.Fatal(err)
	}
	if overview.ManifestSHA256 == "" || overview.View != "checkpoint files" || overview.RootURI == "" || len(overview.ReadRoots) != 1 || overview.ReadRoots[0] != "docs" {
		t.Fatalf("overview=%+v", overview)
	}
	rootPage := readCheckpointPage(t, fixture.session, overview.RootURI)
	if len(rootPage.Entries) != 1 || rootPage.Entries[0].Path != "docs" {
		t.Fatalf("root page=%+v", rootPage)
	}
	page := readCheckpointPage(t, fixture.session, rootPage.Entries[0].URI)
	var largeURI, emptyURI string
	for _, entry := range page.Entries {
		if entry.Path == "docs/large.bin" {
			largeURI = entry.RangeURI
		}
		if entry.Path == "docs/empty.txt" {
			emptyURI = entry.RangeURI
		}
	}
	if largeURI == "" || emptyURI == "" {
		t.Fatalf("file range URIs missing: %+v", page.Entries)
	}
	return largeURI, emptyURI
}

func assertCheckpointRangeBytes(t *testing.T, fixture checkpointResourceFixture, largeURI, emptyURI string) {
	t.Helper()
	first, err := fixture.session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: largeURI})
	if err != nil {
		t.Fatal(err)
	}
	var rangeValue checkpointRangeContent
	if err := json.Unmarshal([]byte(first.Contents[0].Text), &rangeValue); err != nil {
		t.Fatal(err)
	}
	if rangeValue.BlobBase64 == nil {
		t.Fatalf("binary range did not expose base64 bytes: %+v", rangeValue)
	}
	data, err := base64.StdEncoding.DecodeString(*rangeValue.BlobBase64)
	if err != nil || len(data) != 65536 || rangeValue.Offset != 0 || rangeValue.Length != len(data) || rangeValue.NextURI == "" {
		t.Fatalf("range=%+v, data bytes=%d, err=%v", rangeValue, len(data), err)
	}
	digest := sha256.Sum256(fixture.payload)
	if rangeValue.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("range digest=%s", rangeValue.SHA256)
	}
	wholeURI := checkpointPathURI(fixture.host.checkpointReader.Reference(), "files", "docs/large.bin")
	if _, err := fixture.session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: wholeURI}); err == nil {
		t.Fatal("whole-file resource accepted a checkpoint file over 64 KiB")
	}
	empty, err := fixture.session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: emptyURI})
	if err != nil {
		t.Fatal(err)
	}
	var emptyValue map[string]json.RawMessage
	if err := json.Unmarshal([]byte(empty.Contents[0].Text), &emptyValue); err != nil {
		t.Fatal(err)
	}
	if string(emptyValue["text"]) != `""` {
		t.Fatalf("empty range lost its empty text value: %s", empty.Contents[0].Text)
	}
}

func assertCheckpointScopeRefusals(t *testing.T, fixture checkpointResourceFixture) {
	t.Helper()
	ref := fixture.host.checkpointReader.Reference()
	badURIs := []string{
		checkpointPathURI(ref, "files", "outside/secret.txt"),
		"stow://workspaces/" + ref.WorkspaceID + "/v1",
		"stow://workspaces/" + ref.WorkspaceID + "/checkpoints/cp_000000000000000000000000/v1",
		"stow://workspaces/" + ref.WorkspaceID + "/checkpoints/" + ref.CheckpointID + "/ranges/v1/" + base64.RawURLEncoding.EncodeToString([]byte("docs/large.bin")) + "/00/10",
	}
	for _, uri := range badURIs {
		if _, err := fixture.session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri}); err == nil {
			t.Errorf("unexpectedly read %q", uri)
		}
	}
}

func readCheckpointPage(t *testing.T, session *mcp.ClientSession, uri string) checkpointResourcePage {
	t.Helper()
	response, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatal(err)
	}
	var page checkpointResourcePage
	if err := json.Unmarshal([]byte(response.Contents[0].Text), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func checkpointOverviewURI(workspaceID, checkpointID string) string {
	return "stow://workspaces/" + workspaceID + "/checkpoints/" + checkpointID + "/v1"
}

func writeCheckpointTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointResourcesRetainNormalToolsAndRejectBrowser(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := os.WriteFile(filepath.Join(ws.Dir(), "note.txt"), []byte("saved"), 0600); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := ws.CreateCheckpoint(context.Background(), stow.CheckpointOptions{})
	if err != nil {
		t.Fatal(err)
	}
	host, err := NewWorkspaceFiles(Config{RegistryDir: registry, WorkspaceID: ws.ID(), CheckpointID: checkpoint.ID,
		MaxBytes: 1 << 20, MaxFiles: 100, Timeout: time.Second}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	session, closeSessions := workspaceResourceSession(t, host)
	defer closeSessions()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 5 {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	if err := host.EnableBrowser(); err == nil {
		t.Fatal("checkpoint profile accepted the workspace browser")
	}
}
