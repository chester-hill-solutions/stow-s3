package mcpstorage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWorkspacePagedResourcesWalkObservation(t *testing.T) {
	ws, _, session := newPagedWorkspaceFixture(t, 235)
	root := workspacePageURI(ws.ID(), ".")
	first := readWorkspacePage(t, session, root)
	assertFirstWorkspacePage(t, first)
	if err := os.WriteFile(filepath.Join(ws.Dir(), "file-000.txt"), []byte("changed after observation"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), "later.txt"), []byte("later"), 0600); err != nil {
		t.Fatal(err)
	}
	assertObservationWalk(t, session, first)
	assertCurrentWorkspaceFile(t, session, ws.ID(), "file-000.txt", "changed after observation")
	assertCurrentWorkspaceFile(t, session, ws.ID(), "later.txt", "later")
	if _, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: workspacePathURI("tree", ws.ID(), ".")}); err == nil {
		t.Fatal("legacy complete directory unexpectedly accepted overflow")
	}
}

func newPagedWorkspaceFixture(t *testing.T, count int) (*stow.Workspace, *WorkspaceFiles, *mcp.ClientSession) {
	t.Helper()
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("file-%03d.txt", i)
		if err := os.WriteFile(filepath.Join(ws.Dir(), name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	host, err := NewWorkspaceFiles(Config{RegistryDir: registry, WorkspaceID: ws.ID(), MaxBytes: 1, MaxFiles: 1, Timeout: 3 * time.Second}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	session, closeSessions := workspaceResourceSession(t, host)
	t.Cleanup(closeSessions)
	return ws, host, session
}

func assertFirstWorkspacePage(t *testing.T, page workspaceDirectoryPage) {
	t.Helper()
	if page.Page.Offset != 0 || page.Page.Count != 100 || page.Page.NextURI == "" || page.ObservationID == "" || !page.ObservationComplete {
		t.Fatalf("first page=%+v", page.Page)
	}
}

func assertObservationWalk(t *testing.T, session *mcp.ClientSession, first workspaceDirectoryPage) {
	t.Helper()
	all := append([]workspaceEntry(nil), first.Entries...)
	for next := first.Page.NextURI; next != ""; {
		page := readWorkspacePage(t, session, next)
		if page.ObservationID != first.ObservationID || page.Page.Offset != len(all) || page.Page.Count != len(page.Entries) {
			t.Fatalf("page discontinuity: %+v", page.Page)
		}
		all = append(all, page.Entries...)
		next = page.Page.NextURI
	}
	if len(all) != 235 || all[0].Name != "file-000.txt" || all[len(all)-1].Name != "file-234.txt" {
		t.Fatalf("entries=%d first=%q last=%q", len(all), all[0].Name, all[len(all)-1].Name)
	}
}

func assertCurrentWorkspaceFile(t *testing.T, session *mcp.ClientSession, id, name, want string) {
	t.Helper()
	file, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: workspacePathURI("files", id, name)})
	if err != nil || len(file.Contents) != 1 || file.Contents[0].Text != want {
		t.Fatalf("current file=%+v err=%v", file, err)
	}
}

func readWorkspacePage(t *testing.T, session *mcp.ClientSession, uri string) workspaceDirectoryPage {
	t.Helper()
	result, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("read %q: %v", uri, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > workspaceResponseLimit {
		t.Fatalf("encoded response size=%d", len(encoded))
	}
	var page workspaceDirectoryPage
	if len(result.Contents) != 1 || json.Unmarshal([]byte(result.Contents[0].Text), &page) != nil {
		t.Fatalf("invalid page content: %+v", result)
	}
	return page
}

func TestWorkspacePagedResourcesScopeAndCanonicalCursors(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := os.Mkdir(filepath.Join(ws.Dir(), "inputs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(ws.Dir(), "inputs-private"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), "inputs", "ok.txt"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), "inputs-private", "secret.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	host, err := NewWorkspaceFiles(Config{RegistryDir: registry, WorkspaceID: ws.ID(), MaxBytes: 1, MaxFiles: 1, Timeout: time.Second, ReadRoots: []string{"inputs", "inputs"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	session, closeSessions := workspaceResourceSession(t, host)
	defer closeSessions()
	assertWorkspaceOverviewReadRoots(t, session, ws.ID(), "inputs")
	root := readWorkspacePage(t, session, workspacePageURI(ws.ID(), "."))
	if len(root.Entries) != 1 || root.Entries[0].Name != "inputs" {
		t.Fatalf("scoped root entries=%+v", root.Entries)
	}
	inputs := readWorkspacePage(t, session, root.Entries[0].URI)
	if len(inputs.Entries) != 1 || inputs.Entries[0].Name != "ok.txt" {
		t.Fatalf("scoped input entries=%+v", inputs.Entries)
	}
	bad := []string{
		"stow://workspaces/foreign/tree-pages/v1/Lg",
		workspacePageURI(ws.ID(), "inputs/../inputs"),
		workspacePageURI(ws.ID(), "inputs-private"),
		workspacePageURI(ws.ID(), "inputs") + "/e30=",
		workspacePageURI(ws.ID(), "inputs") + "/eyJ2IjoxLCJvIjoieCIsIm4iOjF9=",
	}
	want := ""
	for _, uri := range bad {
		_, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err == nil {
			t.Errorf("invalid page URI succeeded: %q", uri)
			continue
		}
		if want == "" {
			want = err.Error()
		}
		if err.Error() != want {
			t.Errorf("URI %q error=%q, want common category %q", uri, err, want)
		}
	}
}

func assertWorkspaceOverviewReadRoots(t *testing.T, session *mcp.ClientSession, id string, want string) {
	t.Helper()
	result, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "stow://workspaces/" + id + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	var overview workspaceOverview
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &overview); err != nil {
		t.Fatal(err)
	}
	if len(overview.ReadRoots) != 1 || overview.ReadRoots[0] != want {
		t.Fatalf("overview roots=%v", overview.ReadRoots)
	}
}

func TestWorkspacePageCacheExpiryAndAdmission(t *testing.T) {
	ws, host, session := newPagedWorkspaceFixture(t, 101)
	first := readWorkspacePage(t, session, workspacePageURI(ws.ID(), "."))
	if first.Page.NextURI == "" {
		t.Fatal("first page has no continuation")
	}
	host.mu.Lock()
	host.pageNow = func() time.Time { return time.Now().Add(workspaceObservationTTL + time.Second) }
	host.mu.Unlock()
	if _, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: first.Page.NextURI}); err == nil {
		t.Fatal("expired cursor succeeded")
	}

	cacheHost := pageCacheTestHost()
	fillPageCache(cacheHost, workspaceObservationMaxCount, 1, 0)
	if err := cacheHost.reserveObservationScan(); err != nil {
		t.Fatal(err)
	}
	assertPageCacheState(t, cacheHost, workspaceObservationMaxCount-1, true, int64(workspaceObservationMaxCount-1)+workspaceObservationMaxBytes)
	cacheHost.releaseObservationScan()

	fullHost := pageCacheTestHost()
	fillPageCache(fullHost, 4, workspaceObservationMaxBytes, 1)
	if err := fullHost.reserveObservationScan(); err == nil {
		t.Fatal("admitted scan beyond pinned byte budget")
	}
	assertPageCacheState(t, fullHost, 0, false, workspaceObservationMaxCacheBytes)
}

func TestWorkspacePageScratchIsSeparateFromCacheCharge(t *testing.T) {
	ws, host, session := newPagedWorkspaceFixture(t, 101)
	first := readWorkspacePage(t, session, workspacePageURI(ws.ID(), "."))
	host.mu.Lock()
	host.pageMemoryBytes = workspaceObservationMaxCacheBytes
	host.mu.Unlock()
	second := readWorkspacePage(t, session, first.Page.NextURI)
	if second.Page.Offset != 100 {
		t.Fatalf("continuation offset=%d", second.Page.Offset)
	}
	if host.pageScratchBytes != 0 {
		t.Fatalf("scratch charge leaked: %d", host.pageScratchBytes)
	}
	for i := 0; i < workspaceFilesLimit; i++ {
		if err := host.reservePageScratch(); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
	}
	if err := host.reservePageScratch(); err == nil {
		t.Fatal("scratch budget admitted more than eight active pages")
	}
	for i := 0; i < workspaceFilesLimit; i++ {
		host.releasePageScratch()
	}
	if host.pageScratchBytes != 0 {
		t.Fatalf("scratch charge after release=%d", host.pageScratchBytes)
	}
}

func TestWorkspacePagePackingUsesActualContinuationOffset(t *testing.T) {
	registry := t.TempDir()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: registry})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	pathParts := []string{strings.Repeat("a", 190), strings.Repeat("b", 190), strings.Repeat("c", 190), strings.Repeat("d", 190)}
	root, err := os.OpenRoot(ws.Dir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	directory := ""
	for _, part := range pathParts {
		directory = filepath.Join(directory, part)
		if err := root.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	writePackedWorkspaceFiles(t, root, directory)
	host, err := NewWorkspaceFiles(Config{RegistryDir: registry, WorkspaceID: ws.ID(), MaxBytes: 1, MaxFiles: 1, Timeout: 10 * time.Second}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	session, closeSessions := workspaceResourceSession(t, host)
	defer closeSessions()
	resourcePath := strings.Join(pathParts, "/")
	first := readWorkspacePage(t, session, workspacePageURI(ws.ID(), resourcePath))
	if first.Page.Count <= 0 || first.Page.Count >= 100 || first.Page.NextURI == "" {
		t.Fatalf("packing count=%d next=%q", first.Page.Count, first.Page.NextURI)
	}
	collected := append([]workspaceEntry(nil), first.Entries...)
	for next := first.Page.NextURI; next != ""; {
		page := readWorkspacePage(t, session, next)
		if page.Page.Offset != len(collected) || page.Page.Count != len(page.Entries) {
			t.Fatalf("continuation page=%+v", page.Page)
		}
		collected = append(collected, page.Entries...)
		next = page.Page.NextURI
	}
	if len(collected) != 100 {
		t.Fatalf("collected %d entries", len(collected))
	}
}

func writePackedWorkspaceFiles(t *testing.T, root *os.Root, directory string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		name := strings.Repeat("f", 248) + fmt.Sprintf("%02d", i)
		file, err := root.OpenFile(filepath.ToSlash(filepath.Join(directory, name)), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func pageCacheTestHost() *WorkspaceFiles {
	host := &WorkspaceFiles{pageObservations: make(map[string]*workspacePageObservation), pageNow: time.Now}
	host.cond = sync.NewCond(&host.mu)
	return host
}

func fillPageCache(host *WorkspaceFiles, count int, charge int64, active int) {
	host.mu.Lock()
	defer host.mu.Unlock()
	now := time.Now()
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("id-%d", i)
		host.pageObservations[id] = &workspacePageObservation{id: id, charge: charge, active: active, cached: true, lastUsed: now.Add(time.Duration(i) * time.Second), expires: now.Add(workspaceObservationTTL)}
		host.pageMemoryBytes += charge
	}
}

func assertPageCacheState(t *testing.T, host *WorkspaceFiles, entries int, scanning bool, memory int64) {
	t.Helper()
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.pageObservations) != entries || host.pageScanning != scanning || host.pageMemoryBytes != memory {
		t.Fatalf("cache entries=%d scanning=%v memory=%d", len(host.pageObservations), host.pageScanning, host.pageMemoryBytes)
	}
}
