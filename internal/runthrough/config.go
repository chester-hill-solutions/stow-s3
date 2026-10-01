package runthrough

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
)

// Mode describes whether stow runs local-only or with an upstream adapter.
type Mode string

const (
	ModeLocal      Mode = "local"
	ModeRunThrough Mode = "run-through"
)

// Policy controls how reads and writes interact with upstream storage.
type Policy string

const (
	PolicyReadThroughCache Policy = "readThroughCache"
	PolicyMirrorWrites     Policy = "mirrorWrites"
)

// UpstreamConfig holds credentials and endpoint for upstream S3-compatible storage.
type UpstreamConfig struct {
	AllowInsecureHTTP bool
	Endpoint          string
	AccessKey         string
	SecretKey         string
	SessionToken      string
	Region            string
	// Bucket optionally restricts upstream access to a single bucket name.
	Bucket string
	// Addressing is how requests name their bucket. The zero value is
	// AddressingPath, which is what this field was before it existed; an
	// existing UpstreamConfig literal therefore keeps its behavior.
	//
	// It configures how an upstream is addressed, not whether one is used. See
	// Addressing, and ADR 0011: no explicit request, no upstream.
	Addressing Addressing
}

// CachePolicy bounds the separate upstream-derived cache. Zero values disable
// the corresponding limit.
type CachePolicy struct {
	MaxBytes   int64
	MaxObjects int64
	TTL        time.Duration
}

// Config is the full run-through adapter configuration.
type Config struct {
	Policy Policy
	// Authority is the environment's grant. It is an input to the effective
	// authority this adapter gates on rather than the gate itself: see
	// effectiveAuthority.
	//
	// It lives here rather than as a sixth positional argument to NewWithOutbox
	// because the adapter is built from a Config, and a parameter past the ceiling
	// buys no behaviour. What matters is that the grant is resolved once by the
	// caller and handed to both this adapter and the runtime instance, so the two
	// cannot disagree about what was granted. See ADR 0010 decision 2.
	Authority *authority.Authority
	// AllowLiveWrites is an attenuation input, read exactly once when the adapter is
	// built and folded into the effective authority there. The gate consults only the
	// result, never this field.
	//
	// That is the whole of decision 2: two mechanisms answering one question, kept in
	// step by hand.
	// ResourcePolicy narrows Authority per object, and is consulted on every reach this
	// adapter makes. It exists because the propagation funnel runs outside the runtime
	// instance, so without it the retry worker and the admin retry route consult the
	// environment alone. Refused rather than clipped, as in the runtime. A nil pointer
	// means no policy, which is what this adapter has always done.

	ResourcePolicy         *policy.Set
	AllowLiveWrites        bool
	Revalidate             bool
	CacheDir               string
	Upstream               UpstreamConfig
	EvictOnUpstreamMissing bool
	Cache                  CachePolicy
	// Offline is a refusal, not a preference. When it is set the adapter makes no
	// upstream call of any kind: a read that is not in the local store or the cache
	// is a miss, and the cache is served as-is with no revalidation.
	//
	// The distinction matters because the stale-if-error fallback already covers the
	// case this is for, and that fallback is a consequence of error handling, so it
	// cannot be asserted, proven, audited or cost-bounded: nothing states the network
	// was not touched, and a hung upstream turns every read into a timeout. Setting
	// Offline makes the guarantee checkable, and it bounds cost: a read that would
	// have gone upstream is a network call inside a request, and an agent running a
	// thousand of them against a dead endpoint pays for the failure a thousand times.
	Offline bool
}

// effectiveAuthority folds the attenuation inputs into the one value the adapter
// gates on.
//
// A nil Authority means the caller supplied none, which is this adapter's
// historical behaviour: everything permitted, matching the default the runtime
// documents. Narrowing that default is a separate deliberate change, so it is not
// narrowed here.
//
// AllowLiveWrites false withholds UpstreamWrite and nothing else. Reads are
// unaffected: refusing consent to change someone else's data has never been a
// reason to stop reading from them, and a read-through cache over a locally
// authoritative write is the configuration this preserves.
func (c Config) effectiveAuthority() authority.Authority {
	base := authority.All()
	if c.Authority != nil {
		base = *c.Authority
	}
	if !c.AllowLiveWrites {
		base = base.Without(authority.UpstreamWrite)
	}
	return base
}

// FromEnv resolves upstream credentials from environment variables with
// precedence STOW_* > S3_* > AWS_*. The bool is true when endpoint, access
// key, and secret key are all present (minimum signal for run-through).
func (UpstreamConfig) FromEnv() (UpstreamConfig, bool) {
	endpoint := envFirst(
		"STOW_ENDPOINT",
		"S3_ENDPOINT",
		"AWS_ENDPOINT_URL_S3",
		"AWS_ENDPOINT_URL",
		"AWS_ENDPOINT",
	)
	accessKey := envFirst(
		"STOW_ACCESS_KEY_ID",
		"S3_ACCESS_KEY_ID",
		"AWS_ACCESS_KEY_ID",
	)
	secretKey := envFirst(
		"STOW_SECRET_ACCESS_KEY",
		"S3_SECRET_ACCESS_KEY",
		"AWS_SECRET_ACCESS_KEY",
	)
	sessionToken := envFirst(
		"STOW_SESSION_TOKEN",
		"S3_SESSION_TOKEN",
		"AWS_SESSION_TOKEN",
	)
	if endpoint == "" || accessKey == "" || secretKey == "" {
		return UpstreamConfig{}, false
	}
	return UpstreamConfig{
		Endpoint:     endpoint,
		AccessKey:    accessKey,
		SecretKey:    secretKey,
		SessionToken: sessionToken,
		Region: envFirst(
			"STOW_REGION",
			"S3_REGION",
			"AWS_REGION",
			"AWS_DEFAULT_REGION",
		),
		Bucket: envFirst("STOW_BUCKET", "S3_BUCKET"),
		// Resolved leniently: FromEnv reports whether an upstream is configured,
		// and an invalid style is a startup error rather than a reason to decide
		// there is no upstream. ConfigFromEnvChecked is what refuses it, so a
		// misspelling is reported rather than silently changing the addressing of
		// a server that was about to start.
		Addressing: Addressing(envFirst("STOW_UPSTREAM_ADDRESSING")),
	}, true
}

// ConfigFromEnv builds a Config from environment variables.
func ConfigFromEnv() Config {
	upstream, hasUpstream := (UpstreamConfig{}).FromEnv()

	cfg := Config{
		Policy:                 PolicyReadThroughCache,
		Revalidate:             true,
		EvictOnUpstreamMissing: true,
		CacheDir:               os.Getenv("STOW_CACHE_DIR"),
	}
	if hasUpstream {
		cfg.Upstream = upstream
	}

	if p := os.Getenv("STOW_POLICY"); p != "" {
		if policy, ok := ParsePolicy(p); ok {
			cfg.Policy = policy
		} else {
			// Preserve an invalid value so checked startup rejects it and
			// direct callers cannot silently fall back to a live-write policy.
			cfg.Policy = Policy(strings.TrimSpace(p))
		}
	}

	// STOW_OFFLINE is the environment's copy of the flag, and an explicit false is
	// honoured: a server started with --offline=false on a machine whose shell
	// exports STOW_OFFLINE=true is making a choice, and the flag has to win. Only a
	// recognised true/false value is read, because a misspelling must not silently
	// leave the network open on a server whose operator believed it was closed.
	if raw, ok := os.LookupEnv("STOW_OFFLINE"); ok {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "true", "yes", "on":
			cfg.Offline = true
		case "", "0", "false", "no", "off":
			cfg.Offline = false
		}
	}

	// A policy is a routing choice; this flag is the consent to mutate a real
	// provider. The two are deliberately independent: STOW_POLICY=mirrorWrites
	// in an inherited .env must not, on its own, start writing to a shared
	// bucket. See docs/adr/0005-explicit-live-write-consent.md.
	cfg.AllowLiveWrites = envTruthy("STOW_ALLOW_LIVE_WRITES")

	if cache := strings.ToLower(strings.TrimSpace(os.Getenv("STOW_CACHE"))); cache == "revalidate-never" {
		cfg.Revalidate = false
	}
	if value, present, err := parseEnvBool("STOW_REVALIDATE"); present && err == nil {
		cfg.Revalidate = value
	}

	_ = applyCacheEnv(&cfg)
	return cfg
}

// ConfigFromEnvChecked resolves configuration and reports invalid policy values.
// Callers that start a server should use this instead of silently accepting an
// unknown STOW_POLICY value.
func ConfigFromEnvChecked() (Config, error) {
	cfg := ConfigFromEnv()
	if err := applyCacheEnv(&cfg); err != nil {
		return cfg, err
	}
	if raw, present := os.LookupEnv("STOW_REVALIDATE"); present && strings.TrimSpace(raw) != "" {
		if _, _, err := parseEnvBool("STOW_REVALIDATE"); err != nil {
			return cfg, err
		}
	}
	// An unrecognised STOW_OFFLINE is a startup error, on the same reasoning as
	// STOW_REVALIDATE: this flag is a safety property, and a value nobody
	// recognises is a server whose operator believes the network is closed when it
	// is not. That is the one failure worth refusing at startup.
	if raw, present := os.LookupEnv("STOW_OFFLINE"); present && strings.TrimSpace(raw) != "" {
		if _, _, err := parseEnvBool("STOW_OFFLINE"); err != nil {
			return cfg, err
		}
	}
	if raw := strings.TrimSpace(os.Getenv("STOW_POLICY")); raw != "" {
		if _, ok := ParsePolicy(raw); !ok {
			return cfg, fmt.Errorf("invalid STOW_POLICY %q", raw)
		}
	}
	if raw := strings.TrimSpace(os.Getenv("STOW_UPSTREAM_ADDRESSING")); raw != "" {
		if _, ok := ParseAddressing(raw); !ok {
			return cfg, fmt.Errorf("invalid STOW_UPSTREAM_ADDRESSING %q: expected %q or %q",
				raw, AddressingPath, AddressingVirtualHosted)
		}
	}
	return cfg, nil
}

func applyCacheEnv(cfg *Config) error {
	for _, item := range []struct {
		name  string
		field *int64
	}{
		{name: "STOW_CACHE_MAX_BYTES", field: &cfg.Cache.MaxBytes},
		{name: "STOW_CACHE_MAX_OBJECTS", field: &cfg.Cache.MaxObjects},
	} {
		raw, ok := os.LookupEnv(item.name)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || value < 0 {
			return fmt.Errorf("invalid %s %q: expected a non-negative integer", item.name, raw)
		}
		*item.field = value
	}
	if raw, ok := os.LookupEnv("STOW_CACHE_TTL"); ok && strings.TrimSpace(raw) != "" {
		value := strings.TrimSpace(raw)
		var ttl time.Duration
		if value == "0" {
			ttl = 0
		} else {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("invalid STOW_CACHE_TTL %q: expected a non-negative duration", raw)
			}
			ttl = parsed
		}
		if ttl < 0 {
			return fmt.Errorf("invalid STOW_CACHE_TTL %q: expected a non-negative duration", raw)
		}
		cfg.Cache.TTL = ttl
	}
	return nil
}

// ParsePolicy maps a string to a known public Policy.
func ParsePolicy(raw string) (Policy, bool) {
	switch Policy(strings.TrimSpace(raw)) {
	case PolicyReadThroughCache, PolicyMirrorWrites:
		return Policy(strings.TrimSpace(raw)), true
	default:
		return "", false
	}
}

// DetectMode returns the operational mode, and local is what it returns unless
// something asks for run-through by name.
//
// The invariant is one line: no explicit request, no upstream. Ambient
// credentials decide how an explicitly requested upstream is *authenticated*;
// they do not decide whether one is used, because a developer's AWS_* variables
// and a CI runner's are both enough to make stow reach a live provider, and being
// wrong that way reads and writes someone else's bucket. Being wrong the other
// way costs a command-line flag. STOW_MODE=run-through (or the --mode flag, the
// same decision at another layer) is the request.
func DetectMode() Mode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("STOW_MODE"))) {
	case "run-through", "runthrough":
		// The hyphen is optional because losing it is a typo of the same word
		// and the intent is unambiguous. Nothing else is accepted, and the
		// spelling that was briefly allowed here deserves naming: "upstream" is
		// a different word, not a misspelling of this one. It was also the name
		// of the reserved-and-unused start option the 0.2.0 plan removed, so
		// accepting it here would have let exactly the configuration that plan
		// set out to delete select a live provider — the opposite of what this
		// function is for, through a spelling that looks like it was meant.
		return ModeRunThrough
	default:
		// "local", "auto", unset, and anything unrecognized. An unrecognized
		// value is local rather than an error here, because refusing to start is
		// worse than starting local; the startup banner says which mode was
		// chosen, and STOW_MODE is not a place where a typo should stop a dev
		// server from starting.
		return ModeLocal
	}
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func envTruthy(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseEnvBool(key string) (bool, bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return false, false, nil
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, true, nil
	case "0", "false", "no", "off":
		return false, true, nil
	default:
		return false, true, fmt.Errorf("invalid %s %q: expected a boolean", key, raw)
	}
}
