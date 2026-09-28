package main

import (
	"flag"
	"strconv"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

// registerOfflineFlag declares --offline, and keeps the reasoning about why it is
// a bool with a tri-state reader rather than a string out of serve.
//
// The string version was tried first and broke a real invocation: a string flag
// consumes the next argument, so `--offline --ready-fd 3` failed with
// `invalid --offline "--ready-fd"` and pointed at the wrong problem entirely. The
// flag package already accepts `--offline=false`, and Visit distinguishes an
// explicit false from an absent flag, so the tri-state costs nothing.
func registerOfflineFlag(flags *flag.FlagSet) {
	flags.Bool("offline", false,
		"Refuse every upstream call: reads come from the local store and cache, or are misses (unset uses STOW_OFFLINE)")
}

// applyOfflineFlag resolves --offline onto the config, if it was given at all.
//
// Separate from serve because the decision is one concern and serve is a hundred
// others: the flag is a tri-state, and the rule for it is three lines that are easy
// to get wrong and easy to read. Leaving it inline is what pushed serve past the
// function-length gate, and the fix is a seam rather than a shorter comment.
func applyOfflineFlag(flags *flag.FlagSet, cfg *runthrough.Config) {
	if !offlineFlagGiven(flags) {
		return
	}
	offline := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "offline" {
			if parsed, err := strconv.ParseBool(f.Value.String()); err == nil {
				offline = parsed
			}
		}
	})
	cfg.Offline = offline
}

// offlineFlagGiven reports whether --offline appeared on the command line at all.
//
// This is the tri-state, and it is why the flag stays a bool. `--offline` and
// `--offline=false` both set it, and the flag package reports only the ones that
// were set. A string flag would also carry the information and would break
// `--offline --ready-fd 3` by eating the next argument, which is the sort of thing
// that is discovered by a launcher rather than by a test.
//
// The distinction matters because STOW_OFFLINE is a safety property. An absent
// flag must defer to the environment, and only an explicit false may contradict it:
// an operator whose shell exports STOW_OFFLINE=true needs a way to say no on one
// command, and a plain bool that ignored the environment would take that away.
func offlineFlagGiven(flags *flag.FlagSet) bool {
	given := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "offline" {
			given = true
		}
	})
	return given
}
