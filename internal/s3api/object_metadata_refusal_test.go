package s3api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// objectProperties are the standard representation headers the record model has
// no field for. A write that sets one must be refused before it mutates anything,
// because the alternative is a 200 for a property the next HEAD cannot return.
var objectProperties = []string{
	"Cache-Control",
	"Content-Disposition",
	"Content-Encoding",
	"Content-Language",
	"Expires",
}

type metadataHarness struct {
	t      *testing.T
	store  *storage.MemoryStore
	server *httptest.Server
}

func newMetadataHarness(t *testing.T) *metadataHarness {
	t.Helper()
	store := storage.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateBucket(t.Context(), "bucket"); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}
	srv, err := s3api.New(s3api.Config{Store: store, Auth: s3api.DevBypass, Host: "127.0.0.1"})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)
	return &metadataHarness{t: t, store: store, server: server}
}

func (h *metadataHarness) do(method, path string, body io.Reader, headers map[string]string) (*http.Response, string) {
	h.t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, body)
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read body: %v", err)
	}
	return resp, string(raw)
}

func (h *metadataHarness) seed() {
	h.t.Helper()
	for _, key := range []string{"source", "target"} {
		resp, body := h.do(http.MethodPut, "/bucket/"+key, strings.NewReader("original"), nil)
		if resp.StatusCode != http.StatusOK {
			h.t.Fatalf("seed %s = %d %s", key, resp.StatusCode, body)
		}
	}
}

// assertUnchanged proves the refusal happened before the mutation rather than
// after it: the overwrite target still has its bytes, no key was created and no
// multipart upload was opened.
func (h *metadataHarness) assertUnchanged() {
	h.t.Helper()
	resp, body := h.do(http.MethodGet, "/bucket/target", nil, nil)
	if resp.StatusCode != http.StatusOK || body != "original" {
		h.t.Fatalf("refused write changed target: %d %q", resp.StatusCode, body)
	}
	resp, listing := h.do(http.MethodGet, "/bucket?list-type=2", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(listing, "<KeyCount>2</KeyCount>") {
		h.t.Fatalf("refused write changed the object inventory: %d %s", resp.StatusCode, listing)
	}
	resp, uploads := h.do(http.MethodGet, "/bucket?uploads", nil, nil)
	if resp.StatusCode != http.StatusOK || strings.Contains(uploads, "<Upload>") {
		h.t.Fatalf("refused initiation created an upload: %d %s", resp.StatusCode, uploads)
	}
}

func (h *metadataHarness) assertRefused(resp *http.Response, body string) {
	h.t.Helper()
	if resp.StatusCode != http.StatusNotImplemented {
		h.t.Fatalf("want 501, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "<Code>NotImplemented</Code>") {
		h.t.Fatalf("want NotImplemented error code: %s", body)
	}
}

func TestStandardObjectPropertyHeadersAreRefusedBeforeMutation(t *testing.T) {
	for _, header := range objectProperties {
		t.Run(header, func(t *testing.T) {
			for _, shape := range []struct {
				name    string
				method  string
				path    string
				headers map[string]string
			}{
				{"new key", http.MethodPut, "/bucket/new", nil},
				{"overwrite", http.MethodPut, "/bucket/target", nil},
				{"multipart initiation", http.MethodPost, "/bucket/pending?uploads", nil},
				{"copy with replace", http.MethodPut, "/bucket/target", map[string]string{
					"x-amz-copy-source":        "/bucket/source",
					"x-amz-metadata-directive": "REPLACE",
				}},
			} {
				t.Run(shape.name, func(t *testing.T) {
					harness := newMetadataHarness(t)
					harness.seed()
					headers := map[string]string{header: "value"}
					for k, v := range shape.headers {
						headers[k] = v
					}
					resp, body := harness.do(shape.method, shape.path, strings.NewReader("replacement"), headers)
					harness.assertRefused(resp, body)
					harness.assertUnchanged()
				})
			}
		})
	}
}

// A malformed multipart selector falls through to PutObject. It must not turn a
// refused property into silent success by selecting a different write path.
func TestObjectPropertyRefusalCannotBeBypassedWithMultipartSelectors(t *testing.T) {
	for _, query := range []string{"?uploadId=unknown", "?partNumber=1", "?uploadId=unknown&partNumber=1"} {
		for _, header := range objectProperties {
			t.Run(query+"/"+header, func(t *testing.T) {
				harness := newMetadataHarness(t)
				harness.seed()
				resp, body := harness.do(http.MethodPut, "/bucket/new"+query, strings.NewReader("must not publish"), map[string]string{header: "value"})
				harness.assertRefused(resp, body)
				harness.assertUnchanged()
			})
		}
	}
}

// The refusal is scoped to the properties the record cannot carry. User metadata
// and the content type are representable, so they must keep working — a refusal
// that also rejected those would pass the tests above while breaking the feature
// the store does support.
func TestRepresentableObjectMetadataIsStillAccepted(t *testing.T) {
	harness := newMetadataHarness(t)
	resp, body := harness.do(http.MethodPut, "/bucket/documented", strings.NewReader("kept"), map[string]string{
		"Content-Type":       "text/plain",
		"x-amz-meta-project": "stow",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("representable metadata write = %d: %s", resp.StatusCode, body)
	}
	resp, body = harness.do(http.MethodHead, "/bucket/documented", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("head = %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain" {
		t.Errorf("content type = %q, want text/plain", got)
	}
	if got := resp.Header.Get("x-amz-meta-project"); got != "stow" {
		t.Errorf("user metadata = %q, want stow", got)
	}
}

// A read carrying one of these names is not a metadata write. Refusing it would
// break a proxy or a client that sets a cache directive on every request, so the
// scope check lives with the refusal rather than in a caller.
func TestReadRequestsAreNotRefusedForObjectPropertyHeaders(t *testing.T) {
	harness := newMetadataHarness(t)
	harness.seed()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, header := range objectProperties {
			t.Run(method+"/"+header, func(t *testing.T) {
				resp, body := harness.do(method, "/bucket/target", nil, map[string]string{header: "value"})
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("%s with %s = %d, want 200: %s", method, header, resp.StatusCode, body)
				}
			})
		}
	}
}
