package conformance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The live provider matrix is a release gate, so its decision table is covered
// here rather than only exercised by hand. Running the real script keeps the
// test honest about shell behaviour such as exit status and GITHUB_OUTPUT.
type liveProviderResolution struct {
	active   string
	provider string
	reason   string
}

func TestLiveProviderResolve(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("live-provider.sh requires a POSIX shell")
	}
	awsConfig := map[string]string{
		"STOW_ENDPOINT":           "https://s3.us-east-1.amazonaws.com",
		"STOW_ACCESS_KEY_ID":      "live-access-key",
		"STOW_SECRET_ACCESS_KEY":  "live-secret-key",
		"STOW_LIVE_BUCKET":        "stow-live",
		"STOW_LIVE_BUCKET_PREFIX": "ci/live",
	}

	for _, testCase := range []struct {
		name    string
		env     map[string]string
		want    liveProviderResolution
		wantErr bool
	}{
		{
			name: "aws profile against an aws endpoint runs",
			env:  withEnv(awsConfig, "STOW_LIVE_PROFILE", "aws-s3"),
			want: liveProviderResolution{active: "true", provider: "aws-s3", reason: "configured"},
		},
		{
			name: "r2 profile against an r2 endpoint runs",
			env: withEnv(withEnv(awsConfig, "STOW_ENDPOINT", "https://account.r2.cloudflarestorage.com"),
				"STOW_LIVE_PROFILE", "cloudflare-r2-custom"),
			want: liveProviderResolution{active: "true", provider: "cloudflare-r2", reason: "configured"},
		},
		{
			name: "custom endpoint with a port runs as custom",
			env: withEnv(withEnv(awsConfig, "STOW_ENDPOINT", "https://minio.internal:9000"),
				"STOW_LIVE_PROFILE", "cloudflare-r2-custom"),
			want: liveProviderResolution{active: "true", provider: "custom", reason: "configured"},
		},
		{
			name: "aws profile refuses a custom endpoint",
			env:  withEnv(withEnv(awsConfig, "STOW_ENDPOINT", "https://minio.internal:9000"), "STOW_LIVE_PROFILE", "aws-s3"),
			want: liveProviderResolution{active: "false", provider: "none", reason: "endpoint-does-not-match-profile"},
		},
		{
			name: "r2 profile refuses an aws endpoint",
			env:  withEnv(awsConfig, "STOW_LIVE_PROFILE", "cloudflare-r2-custom"),
			want: liveProviderResolution{active: "false", provider: "none", reason: "endpoint-does-not-match-profile"},
		},
		{
			name: "missing configuration skips by default",
			env:  map[string]string{"STOW_LIVE_PROFILE": "aws-s3"},
			want: liveProviderResolution{active: "false", provider: "none", reason: "missing-configuration"},
		},
		{
			name:    "missing configuration fails when the gate requires it",
			env:     withEnv(map[string]string{"STOW_LIVE_PROFILE": "aws-s3"}, "STOW_CONFORMANCE_REQUIRE_CONFIGURED", "true"),
			wantErr: true,
		},
		{
			name: "explicitly requesting the other profile skips",
			env: withEnv(withEnv(awsConfig, "STOW_LIVE_PROFILE", "aws-s3"),
				"STOW_CONFORMANCE_REQUESTED_PROVIDER", "cloudflare-r2-custom"),
			want: liveProviderResolution{active: "false", provider: "none", reason: "profile-not-selected"},
		},
		{
			name: "explicit request against a mismatched endpoint fails",
			env: withEnv(withEnv(withEnv(awsConfig, "STOW_ENDPOINT", "https://minio.internal:9000"),
				"STOW_LIVE_PROFILE", "aws-s3"), "STOW_CONFORMANCE_REQUESTED_PROVIDER", "aws-s3"),
			wantErr: true,
		},
		{
			name: "dry run reports the profile without configuration",
			env:  withEnv(map[string]string{"STOW_LIVE_PROFILE": "aws-s3"}, "STOW_CONFORMANCE_DRY_RUN", "true"),
			want: liveProviderResolution{active: "true", provider: "aws-s3", reason: "dry-run"},
		},
		{
			name:    "plaintext endpoints are rejected",
			env:     withEnv(withEnv(awsConfig, "STOW_ENDPOINT", "http://s3.amazonaws.com"), "STOW_LIVE_PROFILE", "aws-s3"),
			wantErr: true,
		},
		{
			name:    "an unknown profile is rejected",
			env:     map[string]string{"STOW_LIVE_PROFILE": "azure-blob"},
			wantErr: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, stderr, err := resolveLiveProvider(t, testCase.env)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("resolve succeeded with %+v, want failure", got)
				}
			} else {
				if err != nil {
					t.Fatalf("resolve failed: %v (stderr: %s)", err, stderr)
				}
				if got != testCase.want {
					t.Fatalf("resolve = %+v, want %+v", got, testCase.want)
				}
			}
			for _, secret := range []string{"live-access-key", "live-secret-key"} {
				if strings.Contains(stderr, secret) {
					t.Fatalf("resolve leaked %q to stderr: %s", secret, stderr)
				}
			}
		})
	}
}

func withEnv(base map[string]string, name, value string) map[string]string {
	merged := make(map[string]string, len(base)+1)
	for key, existing := range base {
		merged[key] = existing
	}
	merged[name] = value
	return merged
}

func resolveLiveProvider(t *testing.T, env map[string]string) (liveProviderResolution, string, error) {
	t.Helper()
	output := filepath.Join(t.TempDir(), "github-output")
	// A fresh environment keeps ambient STOW_* or AWS_* values in the developer
	// shell from deciding a release gate.
	cmd := exec.Command("./live-provider.sh", "resolve")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GITHUB_OUTPUT=" + output}
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()

	var resolution liveProviderResolution
	if data, readErr := os.ReadFile(output); readErr == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			key, value, found := strings.Cut(line, "=")
			if !found {
				continue
			}
			switch key {
			case "active":
				resolution.active = value
			case "provider":
				resolution.provider = value
			case "reason":
				resolution.reason = value
			}
		}
	}
	return resolution, stderr.String(), err
}

// The provider is classified in resolve, a different process that reports through
// $GITHUB_OUTPUT. Outside CI that goes nowhere, so the documented local invocation
// classifies cleanly: a gate only the workflow running it could satisfy.
func TestLiveProviderTestDerivesTheProviderItCannotInherit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("live-provider.sh requires a POSIX shell")
	}
	script := filepath.Join("live-provider.sh")

	for _, testCase := range []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name: "an r2 endpoint under the r2 profile derives cloudflare-r2",
			env: map[string]string{
				"STOW_ENDPOINT":     "https://6b149553c5075c79bbc132c481f387fe.r2.cloudflarestorage.com",
				"STOW_LIVE_PROFILE": "cloudflare-r2-custom",
			},
		},
		{
			name: "a mismatched profile is refused rather than derived from the endpoint",
			env: map[string]string{
				"STOW_ENDPOINT":     "https://6b149553c5075c79bbc132c481f387fe.r2.cloudflarestorage.com",
				"STOW_LIVE_PROFILE": "aws-s3",
			},
			wantErr: "does not match the aws-s3 profile",
		},
		{
			name: "a plaintext endpoint is refused before the provider matters",
			env: map[string]string{
				"STOW_ENDPOINT":     "http://6b149553c5075c79bbc132c481f387fe.r2.cloudflarestorage.com",
				"STOW_LIVE_PROFILE": "cloudflare-r2-custom",
			},
			wantErr: "must use https",
		},
		{
			name: "neither a profile nor a provider is refused rather than guessed",
			env: map[string]string{
				"STOW_ENDPOINT": "https://6b149553c5075c79bbc132c481f387fe.r2.cloudflarestorage.com",
			},
			wantErr: "STOW_LIVE_PROFILE is too",
		},
		{
			name: "a provider the caller named is trusted over the endpoint",
			env: map[string]string{
				"STOW_ENDPOINT":             "https://6b149553c5075c79bbc132c481f387fe.r2.cloudflarestorage.com",
				"STOW_LIVE_PROFILE":         "aws-s3",
				"STOW_CONFORMANCE_PROVIDER": "cloudflare-r2",
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// Dummy credentials and no disposable flag, so the run fails in the
			// script's own validation. Which message it fails with is the assertion.
			cmd := exec.Command("bash", script, "test")
			cmd.Env = append(os.Environ(),
				"STOW_ACCESS_KEY_ID=derivation-test-key",
				"STOW_SECRET_ACCESS_KEY=derivation-test-secret",
				"STOW_LIVE_BUCKET=stow-live",
				"STOW_LIVE_BUCKET_PREFIX=ci/live",
			)
			cmd.Env = replaceEnv(cmd.Env, testCase.env)
			output, err := cmd.CombinedOutput()

			if testCase.wantErr != "" {
				if err == nil {
					t.Fatalf("the run succeeded, want a refusal containing %q:\n%s", testCase.wantErr, output)
				}
				if !strings.Contains(string(output), testCase.wantErr) {
					t.Fatalf("the refusal does not contain %q:\n%s", testCase.wantErr, output)
				}
				return
			}
			// The deriving cases get past validation and fail later, on the network
			// or the disposable guard. The message this change removes must be absent.
			if strings.Contains(string(output), "STOW_CONFORMANCE_PROVIDER is required") {
				t.Fatalf("the provider was still not derived:\n%s", output)
			}
		})
	}
}

// replaceEnv lets a case express "this variable is absent", which is the state under
// test and the one an inherited environment silently defeats.
func replaceEnv(env []string, overrides map[string]string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		name := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			name = entry[:i]
		}
		if _, replaced := overrides[name]; !replaced {
			kept = append(kept, entry)
		}
	}
	for name, value := range overrides {
		if value == "" {
			continue
		}
		kept = append(kept, name+"="+value)
	}
	return kept
}
