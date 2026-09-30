package conformance_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/chester-hill-solutions/stow-s3/internal/auth"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

type resumeWire struct {
	mu           sync.Mutex
	parts        map[int]int
	partBytes    int64
	rangeBytes   int64
	listPages    int
	completions  int
	dropPart     bool
	dropComplete bool
}

func (w *resumeWire) RoundTrip(request *http.Request) (*http.Response, error) {
	query := request.URL.Query()
	w.mu.Lock()
	drop := false
	if request.Method == http.MethodPut && query.Get("partNumber") != "" {
		number, _ := strconv.Atoi(query.Get("partNumber"))
		w.parts[number]++
		size := request.ContentLength
		if decoded := request.Header.Get("X-Amz-Decoded-Content-Length"); decoded != "" {
			size, _ = strconv.ParseInt(decoded, 10, 64)
		}
		w.partBytes += size
		drop = w.dropPart
		w.dropPart = false
	}
	if request.Method == http.MethodGet && query.Get("uploadId") != "" {
		w.listPages++
	}
	if request.Method == http.MethodPost && query.Get("uploadId") != "" {
		w.completions++
		drop = w.dropComplete
		w.dropComplete = false
	}
	w.mu.Unlock()
	response, err := http.DefaultTransport.RoundTrip(request)
	if err != nil {
		return response, err
	}
	if drop && response.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return nil, errors.New("synthetic reply loss after server response")
	}
	if request.Header.Get("Range") != "" && response.StatusCode == http.StatusPartialContent {
		response.Body = &resumeRangeBody{ReadCloser: response.Body, wire: w}
	}
	return response, nil
}

type resumeRangeBody struct {
	io.ReadCloser
	wire *resumeWire
}

func (b *resumeRangeBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	b.wire.mu.Lock()
	b.wire.rangeBytes += int64(n)
	b.wire.mu.Unlock()
	return n, err
}

type resumeHost struct {
	t            *testing.T
	profile, dir string
	server       *s3api.Server
	endpoint     string
	creds        auth.Credentials
	wire         *resumeWire
	exited       chan error
}

func newResumeHost(t *testing.T, profile string) *resumeHost {
	t.Helper()
	h := &resumeHost{t: t, profile: profile, dir: t.TempDir(), creds: auth.Credentials{AccessKeyID: "AKIACONFORMANCETEST01", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}, wire: &resumeWire{parts: make(map[int]int)}}
	h.start()
	t.Cleanup(h.stop)
	return h
}
func (h *resumeHost) store() storage.Store {
	h.t.Helper()
	if h.profile == "memory" {
		return storage.NewMemoryStore()
	}
	if h.profile == "workspace" {
		store, err := workspace.New(workspace.Options{Root: h.dir, Bucket: "workspace"})
		if err != nil {
			h.t.Fatal(err)
		}
		return store
	}
	backing, err := fs.NewFilesystemStore(h.dir)
	if err != nil {
		h.t.Fatal(err)
	}
	if h.profile == "filesystem" {
		return backing
	}
	instance, err := stowruntime.OpenWithStore(stowruntime.Options{Backend: stowruntime.BackendFilesystem, MaxBytes: 64 << 20, MaxObjects: 1000}, backing, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	adapter, err := stowruntime.NewStoreAdapter(instance)
	if err != nil {
		h.t.Fatal(err)
	}
	return adapter
}
func (h *resumeHost) start() {
	h.t.Helper()
	server, err := s3api.New(s3api.Config{Store: h.store(), Auth: s3api.SigV4Auth(auth.NewVerifier(testRegion), h.creds), Host: "127.0.0.1", Port: 0, Region: testRegion})
	if err != nil {
		h.t.Fatal(err)
	}
	h.server = server
	h.exited = make(chan error, 1)
	go func() { h.exited <- server.ListenAndServe() }()
	deadline := time.Now().Add(2 * time.Second)
	for server.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if server.Addr() == "" {
		h.t.Fatal("resume server failed to bind")
	}
	h.endpoint = "http://" + server.Addr()
}
func (h *resumeHost) stop() {
	if h.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.server.Shutdown(ctx); err != nil {
		h.t.Errorf("resume shutdown: %v", err)
	}
	select {
	case err := <-h.exited:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.t.Errorf("resume server: %v", err)
		}
	case <-ctx.Done():
		h.t.Error("resume server did not stop")
	}
	h.server = nil
}
func (h *resumeHost) restart() { h.stop(); h.start() }
func (h *resumeHost) client() *s3.Client {
	options := newS3Client(h.t, h.endpoint, h.creds, &responseStatusCapture{}).Options()
	options.HTTPClient = &http.Client{Transport: h.wire}
	options.RetryMaxAttempts = 1
	return s3.New(options)
}
