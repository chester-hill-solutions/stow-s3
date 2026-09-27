package runthrough

import (
	"strings"
	"testing"
)

// The addressing style is the one upstream setting that was previously not
// configurable at all: the client was built with UsePathStyle unconditionally.
// A provider that requires virtual-hosted addressing therefore could not be
// reached, and the failure surfaced as a DNS lookup for
// "bucket.endpoint.example" rather than as anything stow could explain.

func TestParseAddressing(t *testing.T) {
	for _, testCase := range []struct {
		raw  string
		want Addressing
		ok   bool
	}{
		{raw: "path", want: AddressingPath, ok: true},
		{raw: "  path  ", want: AddressingPath, ok: true},
		{raw: "PATH", want: AddressingPath, ok: true},
		{raw: "virtual-hosted", want: AddressingVirtualHosted, ok: true},
		{raw: "virtualhosted", want: AddressingVirtualHosted, ok: true},
		{raw: "vhost", want: AddressingVirtualHosted, ok: true},
		// An unset or blank value is the default rather than an error, because
		// an absent variable is not a request for anything.
		{raw: "", want: AddressingPath, ok: true},
		{raw: "   ", want: AddressingPath, ok: true},
		// Anything else is refused rather than guessed at. A typo that silently
		// became path-style would send requests to a host the provider does not
		// serve, which is the same class of failure as the mode default that
		// ADR 0011 removed.
		{raw: "virtual hosted", ok: false},
		{raw: "pathstyle", ok: false},
		{raw: "auto", ok: false},
		{raw: "yes", ok: false},
	} {
		got, ok := ParseAddressing(testCase.raw)
		if ok != testCase.ok {
			t.Errorf("ParseAddressing(%q) ok = %v, want %v", testCase.raw, ok, testCase.ok)
			continue
		}
		if ok && got != testCase.want {
			t.Errorf("ParseAddressing(%q) = %q, want %q", testCase.raw, got, testCase.want)
		}
	}
}

// A zero UpstreamConfig must mean path-style, so every existing construction site
// keeps the behavior it had. This is the assertion that would fail if a new
// field were added without a zero value that preserves the default.
func TestUpstreamConfigZeroValueIsPathStyle(t *testing.T) {
	var cfg UpstreamConfig
	if got := cfg.addressing(); got != AddressingPath {
		t.Fatalf("zero UpstreamConfig addressing = %q, want %q", got, AddressingPath)
	}
	if cfg.UseVirtualHosted() {
		t.Fatal("a zero UpstreamConfig must not request virtual-hosted addressing")
	}
}

// Addressing names how to address a bucket. It is not a request to use an
// upstream, so on its own it must not be able to select one — the invariant
// ADR 0011 states as "no explicit request, no upstream". The environment is
// cleared rather than assumed empty, because a developer shell or a CI runner
// exports these for unrelated tools and a test that passes only when they happen
// to be unset is a test that reports whatever the machine was doing.
func TestAddressingAloneDoesNotSelectAnUpstream(t *testing.T) {
	for _, key := range []string{
		"STOW_ENDPOINT", "S3_ENDPOINT", "AWS_ENDPOINT_URL_S3", "AWS_ENDPOINT_URL", "AWS_ENDPOINT",
		"STOW_ACCESS_KEY_ID", "S3_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID",
		"STOW_SECRET_ACCESS_KEY", "S3_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY",
		"STOW_SESSION_TOKEN", "S3_SESSION_TOKEN", "AWS_SESSION_TOKEN",
	} {
		t.Setenv(key, "")
	}
	if _, ok := (UpstreamConfig{Addressing: AddressingVirtualHosted}).FromEnv(); ok {
		t.Fatal("addressing alone resolved an upstream")
	}
	// The same environment with credentials present does resolve one, which is
	// what makes the assertion above about addressing rather than about the
	// environment being empty for some unrelated reason.
	t.Setenv("STOW_ACCESS_KEY_ID", "AKIAEXAMPLE")
	t.Setenv("STOW_SECRET_ACCESS_KEY", "secret")
	t.Setenv("STOW_ENDPOINT", "https://s3.example")
	if _, ok := (UpstreamConfig{Addressing: AddressingVirtualHosted}).FromEnv(); !ok {
		t.Fatal("credentials alone should still resolve an upstream")
	}
}

func TestAddressingFromEnv(t *testing.T) {
	t.Setenv("STOW_ACCESS_KEY_ID", "AKIAEXAMPLE")
	t.Setenv("STOW_SECRET_ACCESS_KEY", "secret")
	t.Setenv("STOW_ENDPOINT", "https://s3.example")

	t.Setenv("STOW_UPSTREAM_ADDRESSING", "")
	cfg, ok := (UpstreamConfig{}).FromEnv()
	if !ok {
		t.Fatal("expected the upstream environment to resolve")
	}
	if cfg.UseVirtualHosted() {
		t.Fatal("an unset STOW_UPSTREAM_ADDRESSING must leave path-style in place")
	}

	t.Setenv("STOW_UPSTREAM_ADDRESSING", "virtual-hosted")
	cfg, ok = (UpstreamConfig{}).FromEnv()
	if !ok {
		t.Fatal("expected the upstream environment to resolve")
	}
	if !cfg.UseVirtualHosted() {
		t.Fatal("STOW_UPSTREAM_ADDRESSING=virtual-hosted was ignored")
	}
}

// An unrecognized value is refused at startup rather than silently becoming
// path-style. The same reasoning as STOW_POLICY: a server that starts with a
// misspelled addressing style will send every request somewhere the provider
// does not answer, and the operator finds out from a DNS failure.
func TestInvalidAddressingIsRefusedByConfigFromEnvChecked(t *testing.T) {
	t.Setenv("STOW_ACCESS_KEY_ID", "AKIAEXAMPLE")
	t.Setenv("STOW_SECRET_ACCESS_KEY", "secret")
	t.Setenv("STOW_ENDPOINT", "https://s3.example")
	t.Setenv("STOW_UPSTREAM_ADDRESSING", "virtual hosted")

	_, err := ConfigFromEnvChecked()
	if err == nil {
		t.Fatal("expected an invalid addressing style to be refused")
	}
	if !strings.Contains(err.Error(), "STOW_UPSTREAM_ADDRESSING") {
		t.Fatalf("error %q does not name the variable that was wrong", err)
	}
}
