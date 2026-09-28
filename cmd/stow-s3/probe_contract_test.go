package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// This file is the executable form of docs/running-and-probing.md's readiness
// section, and it runs the real binary because the claim is about a process: the
// endpoint a server reports is the address it bound, and a port of 0 is a request
// rather than a promise. Everything below is asserted against a child process, so
// a launcher written in any language can rely on it.

var (
	builtBinaryOnce sync.Once
	builtBinaryPath string
	builtBinaryErr  error
)

// launcherBinary builds stow-s3 once per test run. The claims here are about what
// the process prints and which port it listens on, so there is no way to assert
// them without running it.
func launcherBinary(t *testing.T) string {
	t.Helper()
	builtBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "stow-launcher-test-*")
		if err != nil {
			builtBinaryErr = err
			return
		}
		out := filepath.Join(dir, "stow-s3")
		build := exec.Command("go", "build", "-o", out, ".")
		build.Env = append(os.Environ(), "GOFLAGS=")
		if output, err := build.CombinedOutput(); err != nil {
			builtBinaryErr = fmt.Errorf("go build: %v\n%s", err, output)
			return
		}
		builtBinaryPath = out
	})
	if builtBinaryErr != nil {
		t.Skipf("cannot build the server binary: %v", builtBinaryErr)
	}
	return builtBinaryPath
}

type readyRecord struct {
	ProtocolVersion int    `json:"protocolVersion"`
	BinaryVersion   string `json:"binaryVersion"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	Mode            string `json:"mode"`
	Backend         string `json:"backend"`
}

type launchedServer struct {
	cmd     *exec.Cmd
	record  readyRecord
	stdout  *syncBuffer
	dataDir string
}

// syncBuffer collects the child's stdout on a reader goroutine while the test
// inspects it. A plain buffer here is a data race that only shows up under -race,
// and it would show up only in the test that documents the protocol.
type syncBuffer struct {
	mu    sync.Mutex
	lines strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lines.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lines.String()
}

func (s *launchedServer) stop(t *testing.T) {
	t.Helper()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
	_ = os.RemoveAll(s.dataDir)
}

// launchWithReadyFile starts the server with the versioned readiness channel and
// returns the record it wrote, plus everything it printed.
func launchWithReadyFile(t *testing.T, env ...string) *launchedServer {
	t.Helper()
	binary := launcherBinary(t)
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "ready.json")
	file, err := os.Create(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	cmd := exec.Command(binary, "serve",
		"--port", "0",
		"--mode", "local",
		"--data-dir", filepath.Join(dir, "data"),
		"--ready-fd", "3",
	)
	cmd.Env = append(os.Environ(), env...)
	// Descriptor 3 is the file the server writes its readiness record to. This is
	// the arrangement the document tells a launcher to use.
	cmd.ExtraFiles = []*os.File{file}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	server := &launchedServer{cmd: cmd, dataDir: dir, stdout: &syncBuffer{}}
	t.Cleanup(func() { server.stop(t) })

	// The server writes the banner before the readiness record, so the pipe has to
	// be drained or a large banner could fill it and stall the child.
	go func() {
		_, _ = io.Copy(server.stdout, stdout)
	}()

	deadline := time.Now().Add(20 * time.Second)
	for {
		raw, err := os.ReadFile(readyPath)
		if err == nil && len(raw) > 0 {
			if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &server.record); err != nil {
				t.Fatalf("decode readiness record %q: %v", raw, err)
			}
			// Let the rest of stdout land before it is asserted on, so the
			// absence of a line is the absence of a line rather than a race to
			// read it.
			time.Sleep(200 * time.Millisecond)
			return server
		}
		if time.Now().After(deadline) {
			t.Fatalf("no readiness record within 20s; stdout so far: %s", server.stdout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// The trap that makes a hardcoded port wrong: the server is asked for port 0 and
// reports the address it actually bound, and that address serves.
func TestTheReadinessRecordCarriesTheAddressTheServerBound(t *testing.T) {
	server := launchWithReadyFile(t)
	if server.record.Endpoint == "" {
		t.Fatal("the readiness record has no endpoint")
	}
	if strings.HasSuffix(server.record.Endpoint, ":0/") || strings.HasSuffix(server.record.Endpoint, ":0") {
		t.Fatalf("endpoint = %q, want the bound port rather than the requested one", server.record.Endpoint)
	}
	if server.record.AccessKeyID == "" || server.record.SecretAccessKey == "" {
		t.Error("the record has no credentials; a launcher cannot sign with it")
	}
	if server.record.Region == "" || server.record.BinaryVersion == "" {
		t.Errorf("record is missing region or binaryVersion: %+v", server.record)
	}
	if server.record.ProtocolVersion != 1 {
		t.Errorf("protocolVersion = %d, want 1", server.record.ProtocolVersion)
	}
	// Loopback by default: a server that is not on loopback is unreachable from
	// another machine or another container, and the document says so.
	if !strings.HasPrefix(server.record.Endpoint, "http://127.0.0.1:") {
		t.Errorf("endpoint = %q, want a loopback address: --host defaults to 127.0.0.1", server.record.Endpoint)
	}

	// The endpoint in the record is the one that answers, unsigned, on the
	// diagnostic route. This is the assertion that makes it usable.
	resp, err := http.Get(server.record.Endpoint + "/_stow/health")
	if err != nil {
		t.Fatalf("probe the reported endpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /_stow/health on the reported endpoint = %d, want 200", resp.StatusCode)
	}
	// And the S3 path on the same server is not reachable without signing, which
	// is why a 403 from a bare probe means the wrong path, not a broken server.
	if unsigned, err := http.Get(server.record.Endpoint + "/health"); err == nil {
		defer unsigned.Body.Close()
		if unsigned.StatusCode != http.StatusForbidden {
			t.Errorf("GET /health = %d, want 403", unsigned.StatusCode)
		}
	}
}

// The versioned channel exists so credentials do not travel on stdout, so when it
// is used the legacy line must not also be printed.
func TestTheVersionedChannelKeepsCredentialsOffStdout(t *testing.T) {
	server := launchWithReadyFile(t)
	if strings.Contains(server.stdout.String(), "STOW_READY") {
		t.Errorf("the legacy readiness line was printed alongside the descriptor: %s", server.stdout)
	}
	if strings.Contains(server.stdout.String(), server.record.SecretAccessKey) {
		t.Error("the generated secret key reached stdout")
	}
	if !strings.Contains(server.stdout.String(), "stow mode: local") {
		t.Errorf("the startup banner is missing from stdout: %q", server.stdout)
	}
}

// Without the descriptor the server falls back to one stdout line, and that line
// has to carry the same truth — a launcher that greps for it must not be told a
// port the server did not bind.
func TestTheLegacyLineIsPrintedOnlyWithoutTheDescriptorAndCarriesTheRealPort(t *testing.T) {
	binary := launcherBinary(t)
	cmd := exec.Command(binary, "serve", "--port", "0", "--mode", "local", "--data-dir", t.TempDir())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	scanner := bufio.NewScanner(stdout)
	var line string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "STOW_READY") {
			line = scanner.Text()
			break
		}
	}
	if line == "" {
		t.Fatal("no STOW_READY line on stdout")
	}
	fields := parseReadyFields(line)
	for _, key := range []string{"endpoint", "access_key", "secret_key", "mode"} {
		if fields[key] == "" {
			t.Errorf("the readiness line has no %s: %q", key, line)
		}
	}
	if strings.HasSuffix(fields["endpoint"], ":0") {
		t.Errorf("endpoint = %q, want the bound port", fields["endpoint"])
	}
	resp, err := http.Get(fields["endpoint"] + "/_stow/health")
	if err != nil {
		t.Fatalf("probe the announced endpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("the announced endpoint answered %d, want 200", resp.StatusCode)
	}
}

func parseReadyFields(line string) map[string]string {
	fields := map[string]string{}
	for _, token := range strings.Fields(strings.TrimPrefix(line, "STOW_READY")) {
		key, value, found := strings.Cut(token, "=")
		if found {
			fields[key] = value
		}
	}
	return fields
}

// Local mode is the default and no combination of ambient cloud configuration
// changes it. A launcher that inherits STOW_ENDPOINT from a CI runner must still
// get a local server, and the banner has to say so.
func TestAmbientUpstreamConfigurationDoesNotSelectRunThrough(t *testing.T) {
	server := launchWithReadyFile(t,
		"STOW_ENDPOINT=http://127.0.0.1:1",
		"STOW_ACCESS_KEY_ID=ambient",
		"STOW_SECRET_ACCESS_KEY=ambient",
	)
	if server.record.Mode != "local" {
		t.Errorf("mode = %q, want local: an upstream is never selected by the environment alone", server.record.Mode)
	}
	if !strings.Contains(server.stdout.String(), "upstream: not in use") {
		t.Errorf("the banner does not say the upstream is unused: %q", server.stdout)
	}
	if strings.Contains(server.stdout.String(), "override:") {
		t.Errorf("the banner carries the run-through override note in local mode: %q", server.stdout)
	}
	resp, err := http.Get(server.record.Endpoint + "/_stow/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		Mode        string `json:"mode"`
		WritePolicy string `json:"write_policy"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("decode status: %v (%s)", err, body)
	}
	if status.Mode != "local" || status.WritePolicy != "local-only" {
		t.Errorf("status = %+v, want local mode with a local-only write policy", status)
	}
}
