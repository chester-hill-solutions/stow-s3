package main

import (
	"flag"
	"testing"
)

// --offline has to be a tri-state: absent defers to STOW_OFFLINE, and an explicit
// false contradicts it. STOW_OFFLINE is a safety property, so an operator whose
// shell exports it needs a way to say no on one command, and a flag that ignored the
// environment would take that away.
//
// It stays a *bool* flag rather than becoming a string to carry the third state,
// because a string flag eats the next argument: `--offline --ready-fd 3` failed
// with `invalid --offline "--ready-fd"`. That is the kind of breakage a launcher
// finds and a unit test written against the helper never would, so the tests below
// parse real arguments rather than calling a resolver with a string.

func parseOffline(t *testing.T, args ...string) (bool, bool) {
	t.Helper()
	flags := flag.NewFlagSet("offline-test", flag.ContinueOnError)
	flags.SetOutput(&nullWriter{})
	offline := flags.Bool("offline", false, "")
	flags.Int("ready-fd", -1, "")
	if err := flags.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return *offline, offlineFlagGiven(flags)
}

type nullWriter struct{}

func (nullWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestOfflineFlagIsAbsentWhenItWasNotPassed(t *testing.T) {
	// Absent defers to the environment, and reporting it as "given, false" would
	// silently contradict an exported STOW_OFFLINE=true on every server.
	offline, given := parseOffline(t, "--ready-fd", "3")
	if given {
		t.Error("an absent --offline was reported as given, so it would override the environment with false")
	}
	if offline {
		t.Error("an absent --offline reported true")
	}
}

func TestOfflineFlagIsGivenWhenSetToTrue(t *testing.T) {
	offline, given := parseOffline(t, "--offline", "--ready-fd", "3")
	if !given || !offline {
		t.Errorf("--offline reported given=%v value=%v, want true/true", given, offline)
	}
}

func TestOfflineFlagIsGivenWhenSetToFalse(t *testing.T) {
	// The case that needs the tri-state: an operator overriding an exported
	// STOW_OFFLINE=true on one command.
	offline, given := parseOffline(t, "--offline=false", "--ready-fd", "3")
	if !given {
		t.Error("--offline=false was reported as absent, so it could not override the environment")
	}
	if offline {
		t.Error("--offline=false reported true")
	}
}

func TestOfflineFlagDoesNotEatTheNextArgument(t *testing.T) {
	// The reason this is a bool. As a string flag, --offline consumed --ready-fd
	// and the server refused to start with a message that named the wrong problem.
	flags := flag.NewFlagSet("offline-test", flag.ContinueOnError)
	flags.SetOutput(&nullWriter{})
	flags.Bool("offline", false, "")
	readyFd := flags.Int("ready-fd", -1, "")
	if err := flags.Parse([]string{"--offline", "--ready-fd", "3"}); err != nil {
		t.Fatalf("--offline consumed the following flag: %v", err)
	}
	if *readyFd != 3 {
		t.Errorf("--ready-fd is %d, want 3: --offline consumed its value", *readyFd)
	}
}
