package runthrough

import (
	"fmt"
	"strings"
)

// StartupBanner returns a loud multi-line summary of mode, upstream endpoint,
// cache policy, and write policy for logging at process start.
func StartupBanner(cfg Config, mode Mode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "stow mode: %s\n", mode)

	if mode == ModeRunThrough {
		host := RedactEndpoint(cfg.Upstream.Endpoint)
		if host == "" {
			host = "(not set)"
		}
		fmt.Fprintf(&b, "  upstream: %s (credentials redacted)\n", host)
		if cfg.Upstream.Bucket != "" {
			fmt.Fprintf(&b, "  upstream bucket filter: %s\n", cfg.Upstream.Bucket)
		}
		fmt.Fprintf(&b, "  cache policy: %s\n", cfg.Policy)
		if cfg.Revalidate {
			fmt.Fprintf(&b, "  revalidation: enabled (ETag/Last-Modified)\n")
		} else {
			fmt.Fprintf(&b, "  revalidation: disabled\n")
		}
		if cfg.CacheDir != "" {
			fmt.Fprintf(&b, "  cache dir: %s\n", cfg.CacheDir)
		}
	} else {
		fmt.Fprintf(&b, "  cache policy: none\n")
	}

	writePolicy := EffectiveWritePolicy(cfg)
	switch writePolicy {
	case WritePolicyMirrorWrites:
		fmt.Fprintf(&b, "  WARNING: mirrorWrites propagates supported mutations upstream\n")
	case WritePolicyMirrorWritesDisabled:
		fmt.Fprintf(&b, "  WARNING: mirrorWrites is configured but propagation is disabled, so writes stay local\n")
	}
	fmt.Fprintf(&b, "  write policy: %s\n", writePolicy)
	if mode == ModeLocal {
		// Stated, because the two plausible causes of a local-only server are
		// indistinguishable from the outside: the configuration was never asking
		// for an upstream, or the credentials were absent. A developer who
		// expected run-through and got this would otherwise have no way to tell
		// which half to fix.
		fmt.Fprintf(&b, "  upstream: not in use; run-through requires --mode run-through or STOW_MODE=run-through\n")
	} else {
		fmt.Fprintf(&b, "  override: --mode local or STOW_MODE=local forces local-only\n")
	}
	if mode == ModeRunThrough && !cfg.AllowLiveWrites {
		fmt.Fprintf(&b, "  hint: set STOW_ALLOW_LIVE_WRITES=true to propagate writes upstream\n")
	}
	return b.String()
}
