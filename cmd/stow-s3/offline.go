package main

import (
	"flag"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

// serveConfigInput is the parsed command line, as serve's flags read it, before any
// of it is reconciled with the environment. The pointers are the flag package's own
// storage, which is why this is a struct of them rather than a struct of values: the
// credentials resolver writes back through them.
type serveConfigInput struct {
	AccessKey, SecretKey *string
	MaxBytes, MaxObjects int64
	TTL                  time.Duration
	Offline              *offlineFlag
	Mode                 string
	AdminToken           *string
	AllowLiveWrites      bool
	AllowPublicAdmin     bool
	CacheDir             string
}

// serveRunThroughConfig reconciles the command line with the environment into the
// one config the server runs on.
//
// It is a function rather than inline code in serve for two reasons, and the second
// is the one that matters. serve is over the function-length and complexity
// ceilings already, and every flag added to it pushes both further; inline
// reconciliation is what has been filling them. And the reconciliation is a set of
// precedence rules — the flag beats the environment for the cache limits, for
// offline, and for the cache directory, and the environment fills what the flag did
// not state — which is a body of rules about its own, reviewable on its own rather
// than as fourteen lines between two flag declarations.
func serveRunThroughConfig(input serveConfigInput) runthrough.Config {
	*input.AccessKey, *input.SecretKey = resolveLocalCredentials(*input.AccessKey, *input.SecretKey)
	cfg, err := runthrough.ConfigFromEnvChecked()
	if err != nil {
		log.Fatal(err)
	}
	if err := applyCacheLimits(&cfg, input.MaxBytes, input.MaxObjects, input.TTL); err != nil {
		log.Fatal(err)
	}
	// The flag wins over the environment in both directions, and an explicit false
	// is a real answer: a shell that exports STOW_OFFLINE=true is a default, not an
	// instruction, and a server started with --offline=false is saying so.
	applyOfflineFlag(input.Offline, &cfg)
	if input.AllowPublicAdmin {
		log.Printf("WARNING: --allow-public-admin no longer grants access and is ignored; use --admin-token or STOW_ADMIN_TOKEN to authorize remote admin routes")
	}
	if *input.AdminToken == "" {
		*input.AdminToken = strings.TrimSpace(os.Getenv("STOW_ADMIN_TOKEN"))
	}
	if input.AllowLiveWrites {
		cfg.AllowLiveWrites = true
	}
	if input.CacheDir != "" {
		cfg.CacheDir = input.CacheDir
	}
	return cfg
}

// registerOfflineFlag declares --offline, and keeps the reasoning about why it is a
// bool with a tri-state reader rather than a string out of serve.
//
// The string version was tried first and broke a real invocation: a string flag
// consumes the next argument, so `--offline --ready-fd 3` failed with
// `invalid --offline "--ready-fd"` and pointed at the wrong problem entirely. The
// flag package already accepts `--offline=false`, and a flag that records its own
// presence distinguishes an explicit false from an absent flag, so the tri-state
// costs nothing.
func registerOfflineFlag(flags *flag.FlagSet) *offlineFlag {
	offline := &offlineFlag{}
	flags.Var(offline, "offline",
		"Refuse every upstream call: reads come from the local store and cache, or are misses (unset uses STOW_OFFLINE)")
	return offline
}

// offlineFlag is a bool flag that also records whether it was given.
//
// A plain *bool cannot express the thing this flag has to express. STOW_OFFLINE is
// a safety property, so an operator whose shell exports it needs a way to say no on
// one command, and that means an absent flag and `--offline=false` have to be
// distinguishable. A custom flag.Value records both in one place, and keeps the
// flag from being read back and re-parsed; docs/CODE_STANDARDS.md says what that
// second parse costs when it fails.
type offlineFlag struct {
	value bool
	given bool
}

func (f offlineFlag) String() string { return strconv.FormatBool(f.value) }

// Set records the parsed value and that the flag appeared at all. The receiver is a
// pointer because the flag package copies the value it was given, so a flag that
// only recorded presence in a value receiver would be recorded on a copy.
func (f *offlineFlag) Set(raw string) error {
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return err
	}
	f.value = parsed
	f.given = true
	return nil
}

// IsBoolFlag tells the flag package this accepts the bare `--offline` form with no
// value, which is the whole reason it is a flag.Value rather than a string flag.
func (f *offlineFlag) IsBoolFlag() bool { return true }

// applyOfflineFlag resolves --offline onto the config, if it was given at all.
//
// Separate from serve because the decision is one concern and serve is a hundred
// others: the flag is a tri-state, and the rule for it is three lines that are easy
// to get wrong and easy to read. Leaving it inline is what pushed serve past the
// function-length gate, and the fix is a seam rather than a shorter comment.
//
// An absent flag defers to the environment, which is why this returns rather than
// writing false. Only an explicit value overrides what ConfigFromEnvChecked read,
// and an explicit false is the operator's way out.
func applyOfflineFlag(offline *offlineFlag, cfg *runthrough.Config) {
	if !offline.given {
		return
	}
	cfg.Offline = offline.value
}
