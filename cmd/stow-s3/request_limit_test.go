package main

import (
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
)

func TestNativeRequestLimitFlagsAndReadiness(t *testing.T) {
	defaults := parseServeFlags(nil)
	if defaults.maxRequestBytes != s3api.DefaultMaxRequestBytes {
		t.Fatalf("default=%d", defaults.maxRequestBytes)
	}
	selected := parseServeFlags([]string{"--max-request-bytes", "1234", "--allow-insecure-upstream", "--max-concurrent-requests", "3"})
	if selected.maxRequestBytes != 1234 || !selected.allowInsecureUpstream {
		t.Fatalf("selected=%+v", selected)
	}
	ready := readyMessage(readyDetails{maxRequestBytes: selected.maxRequestBytes, maxConcurrentRequests: selected.maxConcurrentRequests})
	if ready.Capabilities.MaxConcurrentRequests != 3 || defaults.maxConcurrentRequests != s3api.DefaultMaxConcurrentRequests {
		t.Fatal("concurrency limit mismatch")
	}
	if ready.Capabilities.MaxRequestBytes != 1234 {
		t.Fatalf("reported=%d", ready.Capabilities.MaxRequestBytes)
	}
}
