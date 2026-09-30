package runthrough_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

// The upstream client's only inputs are the documented environment variables.
//
// UpstreamConfig is assembled by envFirst over a fixed list of STOW_*, S3_* and
// AWS_* names, and that is the whole of what the documentation says reaches the
// upstream client. It must not also reach config.LoadDefaultConfig, which resolves
// the default AWS chain on top: ~/.aws/config, ~/.aws/credentials, SSO,
// web-identity token files, and the EC2 instance metadata provider.
//
// Most of that chain is inert — region and endpoint are set explicitly and win, and
// both are asserted below as invariants — but reading it is not: naming a profile
// stow never asked for makes client construction fail, so whether stow can reach
// upstream storage at all depends on the machine's AWS configuration.

// scopeRegion pulls the region out of an Authorization header's credential scope,
// which SigV4 spells <access-key>/<date>/<region>/s3/aws4_request inside the
// Credential= parameter.
func scopeRegion(t *testing.T, header string) string {
	t.Helper()
	_, rest, found := strings.Cut(header, "Credential=")
	if !found {
		t.Fatalf("Authorization header %q carries no Credential", header)
	}
	credential, _, _ := strings.Cut(rest, ",")
	scope := strings.Split(credential, "/")
	if len(scope) < 5 {
		t.Fatalf("credential %q is not shaped as <key>/<date>/<region>/<service>/aws4_request", credential)
	}
	return scope[2]
}

// stubUpstream answers every request with 404 and reports the Host and the
// Authorization header, which is where a client reveals what it believes about
// itself.
type stubUpstream struct {
	server        *httptest.Server
	authorization string
	host          string
}

func newStubUpstream(t *testing.T) *stubUpstream {
	t.Helper()
	stub := &stubUpstream{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.authorization = r.Header.Get("Authorization")
		stub.host = r.Host
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *stubUpstream) client(t *testing.T) *runthrough.S3Client {
	t.Helper()
	client, err := runthrough.NewS3Client(runthrough.UpstreamConfig{
		Endpoint:  s.server.URL,
		AccessKey: "AKIAEXAMPLE",
		SecretKey: "secret",
	})
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}
	return client
}

// isolateFromAmbientAWS points the SDK's file and profile lookups at empty
// fixtures and clears every environment variable in the documented list, so the
// shared config is the only thing that could supply a region.
func isolateFromAmbientAWS(t *testing.T, sharedConfig string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", sharedConfig)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	for _, name := range []string{
		"STOW_REGION", "S3_REGION", "AWS_REGION", "AWS_DEFAULT_REGION",
		"AWS_PROFILE", "AWS_DEFAULT_PROFILE",
	} {
		t.Setenv(name, "")
	}
}

func TestUpstreamClientIgnoresAnUnusableProfile(t *testing.T) {
	dir := t.TempDir()
	sharedConfig := filepath.Join(dir, "config")
	if err := os.WriteFile(sharedConfig, []byte("[default]\nregion = eu-west-1\n"), 0o600); err != nil {
		t.Fatalf("write shared config: %v", err)
	}
	isolateFromAmbientAWS(t, sharedConfig)
	t.Setenv("AWS_PROFILE", "a-profile-stow-never-asked-for")

	// The decisive assertion. Building the client must not depend on a profile
	// that stow has no way of knowing about; before the fix this failed with
	// "failed to get shared config profile", so a machine with a stale AWS_PROFILE
	// could not reach upstream storage through stow at all.
	if _, err := runthrough.NewS3Client(runthrough.UpstreamConfig{
		Endpoint:  "http://127.0.0.1:1",
		AccessKey: "AKIAEXAMPLE",
		SecretKey: "secret",
	}); err != nil {
		t.Fatalf("NewS3Client failed because of a profile it never asked for: %v", err)
	}
}

func TestUpstreamClientIgnoresAMissingSharedConfig(t *testing.T) {
	dir := t.TempDir()
	isolateFromAmbientAWS(t, filepath.Join(dir, "no-such-config"))

	if _, err := runthrough.NewS3Client(runthrough.UpstreamConfig{
		Endpoint:  "http://127.0.0.1:1",
		AccessKey: "AKIAEXAMPLE",
		SecretKey: "secret",
	}); err != nil {
		t.Fatalf("NewS3Client failed because of a shared config it never asked for: %v", err)
	}
}

// The two invariants below already held. They are here because they are the
// interesting ones - a shared config *can* set a region and *can* ask for the
// accelerate endpoint, and if a future change to the client options let either
// through, stow would sign for the wrong region or send traffic to an AWS endpoint
// the operator never named. Neither is the bug being fixed; both are the things
// worth noticing if the explicit overrides are ever removed.
func TestUpstreamClientKeepsItsOwnRegionAndEndpoint(t *testing.T) {
	dir := t.TempDir()
	sharedConfig := filepath.Join(dir, "config")
	contents := "[default]\nregion = eu-west-1\ns3_use_accelerate_endpoint = true\n"
	if err := os.WriteFile(sharedConfig, []byte(contents), 0o600); err != nil {
		t.Fatalf("write shared config: %v", err)
	}
	isolateFromAmbientAWS(t, sharedConfig)

	stub := newStubUpstream(t)
	client := stub.client(t)
	_, _ = client.HeadObject(context.Background(), "bucket", "key")

	if stub.authorization == "" {
		t.Fatal("no Authorization header reached the upstream stub")
	}
	if got := scopeRegion(t, stub.authorization); got != "us-east-1" {
		t.Errorf("client signed for region %q, want the built-in default us-east-1", got)
	}
	if want := strings.TrimPrefix(stub.server.URL, "http://"); stub.host != want {
		t.Errorf("request went to host %q, want the configured endpoint %q", stub.host, want)
	}
}
