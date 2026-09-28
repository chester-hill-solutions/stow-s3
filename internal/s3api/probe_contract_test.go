package s3api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/auth"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// This file is the executable form of docs/running-and-probing.md.
//
// It exists because of the one pair of facts an operator gets wrong most often,
// and because nothing asserted them side by side: an unsigned request to a
// diagnostic route is answered, while an unsigned request to an S3 path is
// refused. Both are correct, they mean opposite things, and a probe of the wrong
// path is indistinguishable from an unhealthy server if you only look at the
// status code.

// probeServer is a server with real SigV4 authentication, so "unsigned" in these
// tests means what it means to a caller: no Authorization header at all.
func probeServer(t *testing.T, adminToken string) *httptest.Server {
	t.Helper()
	srv, err := s3api.New(s3api.Config{
		Store:      storage.NewMemoryStore(),
		Auth:       s3api.SigV4Auth(auth.NewVerifier(auth.DefaultRegion), auth.Credentials{AccessKeyID: "probe-key", SecretAccessKey: "probe-secret"}),
		Host:       "127.0.0.1",
		AdminToken: adminToken,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func probeGet(t *testing.T, ts *httptest.Server, path string) (int, string) {
	t.Helper()
	return probeRequest(t, ts, path, "")
}

func probeRequest(t *testing.T, ts *httptest.Server, path, adminToken string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if adminToken != "" {
		req.Header.Set(s3api.AdminTokenHeader, adminToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

// The documented probe surface, unsigned, from loopback. Every row here is in the
// document's table, and a change that breaks one breaks somebody's launcher.
func TestTheDocumentedProbeRoutesAnswerUnsignedFromLoopback(t *testing.T) {
	ts := probeServer(t, "")
	for _, item := range []struct{ path, body string }{
		{"/_stow/health", `"status":"ok"`},
		{"/_stow/status", `"mode":"local"`},
		{"/_stow/metrics", "stow_cache_hits_total"},
	} {
		t.Run(item.path, func(t *testing.T) {
			status, body := probeGet(t, ts, item.path)
			if status != http.StatusOK {
				t.Errorf("status = %d, want 200", status)
			}
			if !strings.Contains(body, item.body) {
				t.Errorf("body = %q, want it to contain %q", body, item.body)
			}
		})
	}
}

// A scrape of the metrics route has to be scrapeable, which is a different
// requirement from answering 200: a Prometheus client rejects the payload on the
// content type.
func TestTheMetricsRouteIsScrapeable(t *testing.T) {
	ts := probeServer(t, "")
	status, body := probeGet(t, ts, "/_stow/metrics")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/_stow/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the Prometheus exposition type", got)
	}
	for _, metric := range []string{"stow_cache_hits_total", "stow_cache_misses_total", "stow_multipart_uploads"} {
		if !strings.Contains(body, metric) {
			t.Errorf("no %s in the exposition", metric)
		}
	}
}

// statusDocument is the subset of /_stow/status a launcher asserts on. Naming the
// fields is the point: a document that says "check status for the mode" is only
// useful if the mode is there.
type statusDocument struct {
	Mode        string `json:"mode"`
	WritePolicy string `json:"write_policy"`
	Version     string `json:"version"`
	UptimeSec   int    `json:"uptime_sec"`
	Listen      string `json:"listen"`
}

func TestTheStatusDocumentCarriesTheFieldsALauncherAssertsOn(t *testing.T) {
	ts := probeServer(t, "")
	_, body := probeGet(t, ts, "/_stow/status")
	var status statusDocument
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatalf("decode status: %v (%s)", err, body)
	}
	if status.Mode == "" || status.Version == "" || status.Listen == "" {
		t.Errorf("status = %+v, want a mode, a version, and a listen address", status)
	}
	if status.WritePolicy != "local-only" {
		t.Errorf("write_policy = %q, want local-only for a server with no upstream", status.WritePolicy)
	}
}

// The finding that prompted this file. From one address, with no credential, the
// diagnostic route answers and the S3 path does not.
func TestAnUnsignedSP3PathIsRefusedWhileTheDiagnosticRouteIsNot(t *testing.T) {
	ts := probeServer(t, "")
	if status, _ := probeGet(t, ts, "/health"); status != http.StatusForbidden {
		t.Errorf("GET /health = %d, want 403: an S3 path requires SigV4", status)
	}
	if status, _ := probeGet(t, ts, "/_stow/health"); status != http.StatusOK {
		t.Errorf("GET /_stow/health = %d, want 200: a diagnostic route does not", status)
	}
}

// Off loopback the read-only routes need the token, which includes another
// container and another host. The refusal is 404 rather than 403 so the route is
// not advertised to a caller that cannot use it.
func TestOffLoopbackTheProbeRoutesNeedTheAdminToken(t *testing.T) {
	const token = "probe-admin-token"
	handler := probeHandler(t, token)
	for _, item := range []struct {
		name   string
		token  string
		status int
	}{
		{"no token", "", http.StatusNotFound},
		{"the configured token", token, http.StatusOK},
		{"a wrong token", "not-the-token", http.StatusNotFound},
	} {
		t.Run(item.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://stow.example/_stow/health", nil)
			req.RemoteAddr = "203.0.113.9:51000"
			if item.token != "" {
				req.Header.Set(s3api.AdminTokenHeader, item.token)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != item.status {
				t.Errorf("status = %d, want %d", recorder.Code, item.status)
			}
		})
	}
}

// probeHandler serves the synthetic off-loopback requests above, which
// httptest.Server cannot produce because it always dials from loopback.
func probeHandler(t *testing.T, adminToken string) http.Handler {
	t.Helper()
	srv, err := s3api.New(s3api.Config{
		Store:      storage.NewMemoryStore(),
		Auth:       s3api.DevBypass,
		Host:       "127.0.0.1",
		AdminToken: adminToken,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv.Handler()
}
