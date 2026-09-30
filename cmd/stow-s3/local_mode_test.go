package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// upstreamRecorder is an upstream that fails the test if it is ever reached.
type upstreamRecorder struct {
	server *httptest.Server
	hits   atomic.Int64
}

func newUpstreamRecorder(t *testing.T) *upstreamRecorder {
	t.Helper()
	rec := &upstreamRecorder{}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rec.hits.Add(1)
		http.Error(w, "upstream must not be reachable from local mode", http.StatusInternalServerError)
	}))
	t.Cleanup(rec.server.Close)
	return rec
}

func (r *upstreamRecorder) requireUntouched(t *testing.T, step string) {
	t.Helper()
	if got := r.hits.Load(); got != 0 {
		t.Fatalf("%s made %d upstream request(s)", step, got)
	}
}

// setHazardousEnv reproduces the environment that used to make stow reach a live
// provider on its own: a .env copied from a staging machine, and the AWS_*
// variables a CI runner or a developer shell has for every other tool.
func setHazardousEnv(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("STOW_ENDPOINT", endpoint)
	t.Setenv("STOW_ACCESS_KEY_ID", "AKIASHAREDSTAGING")
	t.Setenv("STOW_SECRET_ACCESS_KEY", "shared-staging-secret")
	t.Setenv("STOW_POLICY", "mirrorWrites")
}

// setAmbientAWSEnv adds the ambient AWS variables, which is the realistic half of
// the hazard: a machine with AWS credentials exported for unrelated reasons.
func setAmbientAWSEnv(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAAMBIENTMACHINE01")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient-machine-secret")
	t.Setenv("AWS_SESSION_TOKEN", "ambient-session-token")
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-1")
	t.Setenv("AWS_PROFILE", "some-profile-stow-never-asked-for")
	t.Setenv("AWS_ENDPOINT_URL", endpoint)
}

// assertCredentialsAreResolved proves the environment really does carry usable
// upstream credentials, so a later assertion of "no upstream traffic" is not
// passing because the configuration was incomplete.
func assertCredentialsAreResolved(t *testing.T) {
	t.Helper()
	upstream, ok := (runthrough.UpstreamConfig{}).FromEnv()
	if !ok {
		t.Fatal("test setup did not resolve upstream credentials")
	}
	if upstream.AccessKey == "" || upstream.SecretKey == "" || upstream.Endpoint == "" {
		t.Fatalf("resolved upstream credentials are incomplete: %+v", upstream)
	}
}

// assertPolicyIsInert proves the run-through *policy* was configured, so a
// later assertion of "no propagation" is not passing because the policy was
// never set.
func assertPolicyIsInert(t *testing.T) {
	t.Helper()
	cfg := runthrough.ConfigFromEnv()
	if cfg.Policy != runthrough.PolicyMirrorWrites {
		t.Fatalf("policy = %q, want mirrorWrites", cfg.Policy)
	}
	if cfg.AllowLiveWrites {
		t.Fatal("mirrorWrites granted live-write consent on its own")
	}
	if got := runthrough.EffectiveWritePolicy(cfg); got != runthrough.WritePolicyMirrorWritesDisabled {
		t.Fatalf("write policy = %q, want %q", got, runthrough.WritePolicyMirrorWritesDisabled)
	}
}

// startLocalServer brings up the same wiring stow serve uses, in local mode.
func startLocalServer(t *testing.T, dataDir string, cfg runthrough.Config) *httptest.Server {
	t.Helper()
	store, adapter, err := buildStore(runthrough.DetectMode(), runtime.BackendMemory, dataDir, cfg)
	if err != nil {
		t.Fatalf("build store: %v", err)
	}
	if adapter != nil {
		t.Fatal("local mode constructed a run-through adapter")
	}
	srv, err := s3api.New(s3api.Config{Store: store, Auth: s3api.DevBypass, Host: "127.0.0.1", Port: 0})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	local := httptest.NewServer(srv.Handler())
	t.Cleanup(local.Close)
	return local
}

// request performs one S3 call and fails the test on an error status.
func request(t *testing.T, base, method, path, body string) *http.Response {
	t.Helper()
	var reader io.Reader = strings.NewReader(body)
	req, err := http.NewRequestWithContext(context.Background(), method, base+path, reader)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != "" {
		req.ContentLength = int64(len(body))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		t.Fatalf("%s %s: status %d", method, path, resp.StatusCode)
	}
	return resp
}

// Local is the default, and ambient credentials do not change that.
//
// The hazard: a developer's shell, or a CI runner, has AWS credentials exported
// for every other tool on the machine, so whether stow reaches a live provider
// must depend on what the user asked for, not on the machine's environment.
//
// The invariant is one line: no explicit run-through request, no upstream. Two
// things are asserted, because the hazard is only real if both are true — the
// credentials really do resolve (so the test is not passing because the
// configuration was incomplete), and the run-through policy really is configured
// (so the propagation assertions are not passing because the policy was absent).
func TestAmbientCredentialsDoNotSelectRunThrough(t *testing.T) {
	upstream := newUpstreamRecorder(t)
	setHazardousEnv(t, upstream.server.URL)
	setAmbientAWSEnv(t, upstream.server.URL)
	assertCredentialsAreResolved(t)
	assertPolicyIsInert(t)

	// Nothing asked for run-through, so nothing gets it.
	if mode := runthrough.DetectMode(); mode != runthrough.ModeLocal {
		t.Fatalf("DetectMode with ambient credentials = %q, want local", mode)
	}

	dataDir := t.TempDir()
	cfg := runthrough.ConfigFromEnv()
	cfg.CacheDir = filepath.Join(dataDir, "cache")
	local := startLocalServer(t, dataDir, cfg)

	// Every operation that would reach upstream if the store were an adapter.
	steps := []struct{ method, path, body string }{
		{http.MethodPut, "/agent-bucket", ""},
		{http.MethodPut, "/agent-bucket/input.json", `{"task":"summarize"}`},
		{http.MethodGet, "/agent-bucket/input.json", ""},
		{http.MethodHead, "/agent-bucket/input.json", ""},
		{http.MethodGet, "/agent-bucket?list-type=2", ""},
		{http.MethodDelete, "/agent-bucket/input.json", ""},
	}
	for _, step := range steps {
		resp := request(t, local.URL, step.method, step.path, step.body)
		resp.Body.Close()
		upstream.requireUntouched(t, step.method+" "+step.path)
	}

	// A server that silently did nothing locally would also make zero upstream
	// requests, so prove the data really round-tripped through local memory.
	body := "local-content"
	request(t, local.URL, http.MethodPut, "/agent-bucket/verify.json", body).Body.Close()
	resp := request(t, local.URL, http.MethodGet, "/agent-bucket/verify.json", "")
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read verify body: %v", err)
	}
	if string(got) != body {
		t.Fatalf("object body = %q, want %q", got, body)
	}
	upstream.requireUntouched(t, "verify read")
}

// The AWS_* variables alone, with no stow configuration at all.
//
// The staging .env case above is a deliberate act; this is not. A machine with
// AWS credentials exported for the CLI, the SDK and everything else has, without
// anyone intending stow to use them, exactly the variables UpstreamConfig.FromEnv
// falls back to. It is the same hazard with nothing configured at all, which is
// why it is asserted separately.
func TestBareAWSEnvironmentVariablesDoNotSelectRunThrough(t *testing.T) {
	upstream := newUpstreamRecorder(t)
	setAmbientAWSEnv(t, upstream.server.URL)
	assertCredentialsAreResolved(t)

	if mode := runthrough.DetectMode(); mode != runthrough.ModeLocal {
		t.Fatalf("DetectMode with only AWS_* set = %q, want local", mode)
	}
	store, adapter, err := buildStore(runthrough.DetectMode(), runtime.BackendMemory, t.TempDir(), runthrough.ConfigFromEnv())
	if err != nil {
		t.Fatalf("build store: %v", err)
	}
	if adapter != nil {
		t.Fatal("an AWS_* environment alone built a run-through adapter")
	}
	if _, ok := store.(*storage.MemoryStore); !ok {
		t.Fatalf("store = %T, want the plain local memory store", store)
	}
	upstream.requireUntouched(t, "build store with AWS credentials present")
}

// Run-through is reachable, and only by asking.
//
// The default being safe is only useful if the thing it replaced is still
// available, so this asserts the request works: STOW_MODE=run-through with
// credentials present really does select run-through. Without it, "local by
// default" could be satisfied by a build that cannot reach upstream at all, and
// the default would be a removal rather than a change.
func TestRunThroughIsReachableByExplicitRequest(t *testing.T) {
	upstream := newUpstreamRecorder(t)
	setHazardousEnv(t, upstream.server.URL)
	t.Setenv("STOW_MODE", "run-through")

	if mode := runthrough.DetectMode(); mode != runthrough.ModeRunThrough {
		t.Fatalf("DetectMode with STOW_MODE=run-through = %q, want run-through", mode)
	}
}

// STOW_MODE=local still forces local, so the explicit request is reversible.
func TestExplicitLocalModeIsStillLocal(t *testing.T) {
	upstream := newUpstreamRecorder(t)
	setHazardousEnv(t, upstream.server.URL)
	assertCredentialsAreResolved(t)
	t.Setenv("STOW_MODE", "local")

	if mode := runthrough.DetectMode(); mode != runthrough.ModeLocal {
		t.Fatalf("STOW_MODE=local did not force local mode: got %q", mode)
	}

	dataDir := t.TempDir()
	cfg := runthrough.ConfigFromEnv()
	cfg.CacheDir = filepath.Join(dataDir, "cache")
	local := startLocalServer(t, dataDir, cfg)
	request(t, local.URL, http.MethodPut, "/agent-bucket", "").Body.Close()
	resp := request(t, local.URL, http.MethodPut, "/agent-bucket/key", "body")
	resp.Body.Close()
	upstream.requireUntouched(t, "explicit local mode")
}

// buildStore must not build an upstream client in local mode even when the
// configuration is the most aggressive the tool accepts, which now includes live
// write consent — the one thing that used to be the only thing standing between
// this configuration and a live write.
func TestBuildStoreIgnoresUpstreamConfigInLocalMode(t *testing.T) {
	setHazardousEnv(t, "https://upstream.example")
	t.Setenv("STOW_ALLOW_LIVE_WRITES", "true")

	cfg := runthrough.ConfigFromEnv()
	dataDir := t.TempDir()
	cfg.CacheDir = filepath.Join(dataDir, "cache")

	store, adapter, err := buildStore(runthrough.DetectMode(), runtime.BackendMemory, dataDir, cfg)
	if err != nil {
		t.Fatalf("build store: %v", err)
	}
	if adapter != nil {
		t.Fatal("local mode built a run-through adapter despite live-write consent")
	}
	if _, ok := store.(*storage.MemoryStore); !ok {
		t.Fatalf("local mode store = %T, want *storage.MemoryStore", store)
	}
}
