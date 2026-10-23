package main

import (
	"fmt"
	"net"
	"strings"
)

// The exposure posture of a listener: loopback needs no decision, and any other
// host needs the consent, the credentials and the token that ADR 0021 requires.
// TLS termination belongs to the host adapter, not this process (ADR 0016).

// listenHostIsLoopback reports whether a listen host reaches nothing but this
// machine. Empty and wildcard hosts select every interface, and a name that is
// not "localhost" cannot be proven loopback from here.
func listenHostIsLoopback(host string) bool {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(trimmed, "[]"))
	return ip != nil && ip.IsLoopback()
}

// validateRemoteExposure refuses a listener that exposes the store beyond
// loopback without the consent and provisioning it needs. Every refusal names the
// flag that resolves it.
func validateRemoteExposure(options serveFlags) error {
	if listenHostIsLoopback(options.host) {
		return nil
	}
	if !options.allowInsecureRemote {
		return fmt.Errorf("refusing to serve on %q: it is not a loopback address, so this store would be reachable from another host; terminate TLS in front of this process and pass --allow-insecure-remote to accept that, or leave --host at 127.0.0.1 (docs/running-and-probing.md)", options.host)
	}
	if options.accessKey == "" || options.secretKey == "" {
		return fmt.Errorf("serving on %q requires --access-key and --secret-key (or STOW_LOCAL_ACCESS_KEY_ID and STOW_LOCAL_SECRET_ACCESS_KEY): without them this process generates a pair and reports it only to its own stdout, which no remote client can read", options.host)
	}
	if options.adminToken == "" {
		return fmt.Errorf("serving on %q requires --admin-token (or STOW_ADMIN_TOKEN): without one the diagnostic and metrics routes answer 404 to every caller that is not loopback, so health, status and inspection are unavailable to the deployment", options.host)
	}
	return nil
}
