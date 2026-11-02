package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestListenHostIsLoopback(t *testing.T) {
	cases := []struct {
		host     string
		loopback bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"[::1]", true},
		{"localhost", true},
		{"LOCALHOST", true},
		{"127.0.0.5", true},
		{"", true},
		{"0.0.0.0", false},
		{"::", false},
		{"[::]", false},
		{"192.168.1.5", false},
		{"store.internal", false},
		{"  ", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.host, func(t *testing.T) {
			if got := listenHostIsLoopback(testCase.host); got != testCase.loopback {
				t.Errorf("listenHostIsLoopback(%q) = %t, want %t", testCase.host, got, testCase.loopback)
			}
		})
	}
}

func TestValidateRemoteExposure(t *testing.T) {
	provisioned := serveFlags{accessKey: "key", secretKey: "secret", adminToken: "token"}
	cases := []struct {
		name        string
		options     serveFlags
		wantMessage string
	}{
		{
			name:    "loopback needs nothing",
			options: serveFlags{host: "127.0.0.1"},
		},
		{
			name:    "localhost needs nothing",
			options: serveFlags{host: "localhost"},
		},
		{
			name:        "a wildcard bind is refused until it is consented to",
			options:     serveFlags{host: "0.0.0.0"},
			wantMessage: "--allow-insecure-remote",
		},
		{
			name:        "a hostname bind is refused until it is consented to",
			options:     serveFlags{host: "store.internal"},
			wantMessage: "--allow-insecure-remote",
		},
		{
			name:        "consent without credentials is refused",
			options:     serveFlags{host: "0.0.0.0", allowInsecureRemote: true},
			wantMessage: "--access-key and --secret-key",
		},
		{
			name:        "consent without an admin token is refused",
			options:     serveFlags{host: "0.0.0.0", allowInsecureRemote: true, accessKey: "key", secretKey: "secret"},
			wantMessage: "--admin-token",
		},
		{
			name:    "an empty host uses the server loopback default",
			options: serveFlags{},
		},
		{
			name:        "whitespace is not an admin token",
			options:     serveFlags{host: "0.0.0.0", allowInsecureRemote: true, accessKey: "key", secretKey: "secret", adminToken: "   "},
			wantMessage: "--admin-token",
		},
		{
			name:    "consent with provisioning is allowed",
			options: serveFlags{host: "0.0.0.0", allowInsecureRemote: true, accessKey: provisioned.accessKey, secretKey: provisioned.secretKey, adminToken: provisioned.adminToken},
		},
		{
			name:    "loopback with consent is still allowed",
			options: serveFlags{host: "127.0.0.1", allowInsecureRemote: true},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateRemoteExposure(testCase.options)
			if testCase.wantMessage == "" {
				if err != nil {
					t.Fatalf("validateRemoteExposure = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateRemoteExposure = nil, want a refusal naming %s", testCase.wantMessage)
			}
			if !strings.Contains(err.Error(), testCase.wantMessage) {
				t.Errorf("refusal = %q, want it to name %s", err, testCase.wantMessage)
			}
		})
	}
}

// envWithout drops the named variables, so a developer's own STOW_* settings
// cannot stand in for the provisioning a test is asserting is missing. Appending
// an empty value would not do it: the first match wins, not the last.
func envWithout(keys ...string) []string {
	drop := map[string]bool{}
	for _, key := range keys {
		drop[key] = true
	}
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if drop[name] {
			continue
		}
		env = append(env, entry)
	}
	return env
}

// The refusal has to happen before the listen, or the server has already opened
// the socket it is refusing to serve on. This runs the real binary, because the
// claim is about a process's exit and its message.
func TestServeRefusesAWildcardBindWithoutConsent(t *testing.T) {
	expectRemoteRefusal(t, []string{"--host", "0.0.0.0"}, nil, "--allow-insecure-remote")
}

func TestServeRefusesARemoteBindWithoutProvisionedCredentials(t *testing.T) {
	expectRemoteRefusal(t, []string{"--host", "192.0.2.1", "--allow-insecure-remote"}, nil, "--access-key and --secret-key")
}

func TestServeRefusesBlankAdminTokensFromFlagsAndEnvironment(t *testing.T) {
	for _, source := range []string{"flag", "environment"} {
		t.Run(source, func(t *testing.T) {
			args := []string{"--host", "192.0.2.1", "--allow-insecure-remote", "--access-key", "fixture-key", "--secret-key", "fixture-secret"}
			var env []string
			if source == "flag" {
				args = append(args, "--admin-token", "   ")
			} else {
				env = []string{"STOW_ADMIN_TOKEN=   "}
			}
			expectRemoteRefusal(t, args, env, "--admin-token")
		})
	}
}

func expectRemoteRefusal(t *testing.T, args, env []string, message string) {
	t.Helper()
	binary := launcherBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := append([]string{"serve", "--port", "0", "--data-dir", t.TempDir()}, args...)
	cmd := exec.CommandContext(ctx, binary, command...)
	cmd.Env = append(envWithout("STOW_LOCAL_ACCESS_KEY_ID", "STOW_LOCAL_SECRET_ACCESS_KEY", "STOW_ADMIN_TOKEN"), env...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("serve did not refuse before the deadline; the owned process was stopped")
	}
	if err == nil {
		t.Fatalf("serve started despite missing consent/provisioning: %s", output)
	}
	if !strings.Contains(string(output), message) {
		t.Errorf("refusal does not name %s: %s", message, output)
	}
	if strings.Contains(string(output), "STOW_READY") {
		t.Errorf("server announced readiness after refusing: %s", output)
	}
}
