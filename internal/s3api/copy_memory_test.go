package s3api_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

// A metadata-replacement copy must stream the source rather than materialise it.
//
// The source is 8 MiB and lives in a filesystem store, which stages writes to a
// temporary file. The bytes only have to be buffered if some layer reads the
// whole object to learn something it already knows (its length), so the budget
// below sits far under the object size and far above what a stream costs.
func TestCopyReplaceDoesNotBufferTheWholeSource(t *testing.T) {
	const size = 8 << 20
	server := copyMemoryServer(t, 1<<30)
	copyMemoryPut(t, server, "/assets/source", bytes.Repeat([]byte("s"), size), nil)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	status, body := copyMemoryCopy(t, server, "/assets/target", "/assets/source", "REPLACE", nil)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if status != http.StatusOK {
		t.Fatalf("REPLACE copy: %d %s", status, body)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("REPLACE copy allocated %d bytes for a %d byte source", allocated, size)
	if allocated > size/2 {
		t.Fatalf("REPLACE copy allocated %d bytes for a %d byte source; the source is being buffered whole", allocated, size)
	}
}

// A copy that cannot fit the session budget is refused before the source is
// staged, so an oversized copy costs a quota check rather than the object.
func TestCopyReplaceRefusesOversizedCopyWithoutBuffering(t *testing.T) {
	const size = 8 << 20
	server := copyMemoryServer(t, size)
	copyMemoryPut(t, server, "/assets/source", bytes.Repeat([]byte("s"), size), nil)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	status, body := copyMemoryCopy(t, server, "/assets/target", "/assets/source", "REPLACE", nil)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if status != http.StatusInsufficientStorage {
		t.Fatalf("oversized copy status = %d %s, want %d", status, body, http.StatusInsufficientStorage)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("refused REPLACE copy allocated %d bytes", allocated)
	if allocated > size/2 {
		t.Fatalf("refused copy allocated %d bytes; the source is being buffered before the quota is consulted", allocated)
	}
	if got := copyMemoryGetStatus(t, server, "/assets/target"); got != http.StatusNotFound {
		t.Fatalf("refused copy published a destination: %d", got)
	}
}

// A REPLACE copy onto the source's own key must publish the version it captured,
// not a truncated or empty object. The source reader is open on the version being
// replaced, so a backend that published before consuming it would corrupt itself.
func TestCopyReplaceOntoItselfPublishesTheCapturedVersion(t *testing.T) {
	const size = 4 << 20
	server := copyMemoryServer(t, 1<<30)
	payload := bytes.Repeat([]byte("a"), size)
	copyMemoryPut(t, server, "/assets/self", payload, map[string]string{"X-Amz-Meta-Project": "one"})

	status, body := copyMemoryCopy(t, server, "/assets/self", "/assets/self", "REPLACE", map[string]string{"X-Amz-Meta-Project": "two"})
	if status != http.StatusOK {
		t.Fatalf("self REPLACE copy: %d %s", status, body)
	}
	response, err := http.Get(server.URL + "/assets/self")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET self: %d", response.StatusCode)
	}
	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != size || !bytes.Equal(got, payload) {
		t.Fatalf("self copy body is %d bytes, want %d", len(got), size)
	}
	if project := response.Header.Get("X-Amz-Meta-Project"); project != "two" {
		t.Fatalf("self copy metadata project = %q, want two", project)
	}
}

func copyMemoryServer(t *testing.T, maxBytes int64) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	store, err := fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := stowruntime.OpenWithStore(stowruntime.Options{MaxBytes: maxBytes, MaxObjects: 16}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.CreateBucket(context.Background(), "assets"); err != nil {
		t.Fatal(err)
	}
	adapter, err := stowruntime.NewStoreAdapter(instance)
	if err != nil {
		t.Fatal(err)
	}
	server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass, Host: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(server.Handler())
	t.Cleanup(endpoint.Close)
	return endpoint
}

func copyMemoryPut(t *testing.T, server *httptest.Server, path string, body []byte, headers map[string]string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPut, server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(response.Body)
		t.Fatalf("PUT %s: %d %s", path, response.StatusCode, message)
	}
}

func copyMemoryCopy(t *testing.T, server *httptest.Server, destination, source, directive string, headers map[string]string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPut, server.URL+destination, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Amz-Copy-Source", source)
	if directive != "" {
		request.Header.Set("X-Amz-Metadata-Directive", directive)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

func copyMemoryGetStatus(t *testing.T, server *httptest.Server, path string) int {
	t.Helper()
	response, err := http.Get(server.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}
