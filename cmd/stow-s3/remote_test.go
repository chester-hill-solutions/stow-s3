package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
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
		{"", false},
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
	binary := launcherBinary(t)
	cmd := exec.Command(binary, "serve", "--host", "0.0.0.0", "--port", "0", "--data-dir", t.TempDir())
	cmd.Env = envWithout("STOW_LOCAL_ACCESS_KEY_ID", "STOW_LOCAL_SECRET_ACCESS_KEY", "STOW_ADMIN_TOKEN")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("serve started on a wildcard address with no consent: %s", output)
	}
	if !strings.Contains(string(output), "--allow-insecure-remote") {
		t.Errorf("the refusal does not name the flag that resolves it: %s", output)
	}
	if strings.Contains(string(output), "STOW_READY") {
		t.Errorf("the server announced readiness after refusing: %s", output)
	}
}

// A remote bind with consent still refuses when the credentials are missing,
// because a generated pair is reported only to this process's stdout.
func TestServeRefusesARemoteBindWithoutProvisionedCredentials(t *testing.T) {
	binary := launcherBinary(t)
	cmd := exec.Command(binary, "serve", "--host", "192.0.2.1", "--port", "0", "--allow-insecure-remote", "--data-dir", t.TempDir())
	cmd.Env = envWithout("STOW_LOCAL_ACCESS_KEY_ID", "STOW_LOCAL_SECRET_ACCESS_KEY", "STOW_ADMIN_TOKEN")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("serve started without credentials: %s", output)
	}
	if !strings.Contains(string(output), "--access-key and --secret-key") {
		t.Errorf("the refusal does not name the credentials: %s", output)
	}
}
