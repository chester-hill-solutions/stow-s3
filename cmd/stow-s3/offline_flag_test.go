package main

import (
	"flag"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

// --offline has to be a tri-state: absent defers to STOW_OFFLINE, and an explicit
// false contradicts it. STOW_OFFLINE is a safety property, so an operator whose
// shell exports it needs a way to say no on one command, and a flag that ignored the
// environment would take that away.
// It stays a bool flag rather than becoming a string to carry the third state,
// because a string flag eats the next argument: `--offline --ready-fd 3` failed
// with `invalid --offline "--ready-fd"`. That is the kind of breakage a launcher
// finds and a unit test written against the helper never would, so the tests below
// parse real arguments and run the real resolver against a real config.

func parseOffline(t *testing.T, args ...string) (*runthrough.Config, error) {
	t.Helper()
	flags := flag.NewFlagSet("offline-test", flag.ContinueOnError)
	flags.SetOutput(&nullWriter{})
	offline := registerOfflineFlag(flags)
	flags.Int("ready-fd", -1, "")
	err := flags.Parse(args)
	// The environment value is what the flag has to be able to contradict, so it is
	// seeded here rather than left false and irrelevant.
	cfg := &runthrough.Config{Offline: true}
	applyOfflineFlag(offline, cfg)
	return cfg, err
}

type nullWriter struct{}

func (nullWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestOfflineFlagIsAbsentWhenItWasNotPassed(t *testing.T) {
	// Absent defers to the environment. The config starts at Offline true here, the
	// way an exported STOW_OFFLINE=true leaves it, so a resolver that wrote false
	// when the flag was absent would show up as a flipped value rather than as a
	// value that happens to be right.
	cfg, err := parseOffline(t, "--ready-fd", "3")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.Offline {
		t.Error("an absent --offline overrode an environment that set it, so the flag cannot defer")
	}
}

func TestOfflineFlagIsGivenWhenSetToTrue(t *testing.T) {
	cfg, err := parseOffline(t, "--offline", "--ready-fd", "3")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.Offline {
		t.Error("--offline did not close the network")
	}
}

func TestOfflineFlagIsGivenWhenSetToFalse(t *testing.T) {
	// The case that needs the tri-state: an operator overriding an exported
	// STOW_OFFLINE=true on one command. Without the tri-state the only options are
	// "always on" and "always off", and the second one loses.
	cfg, err := parseOffline(t, "--offline=false", "--ready-fd", "3")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Offline {
		t.Error("--offline=false did not open the network, so it cannot override an exported STOW_OFFLINE")
	}
}

func TestOfflineFlagDoesNotEatTheNextArgument(t *testing.T) {
	// The reason this is a bool. As a string flag, --offline consumed --ready-fd
	// and the server refused to start with a message that named the wrong problem.
	flags := flag.NewFlagSet("offline-test", flag.ContinueOnError)
	flags.SetOutput(&nullWriter{})
	registerOfflineFlag(flags)
	readyFd := flags.Int("ready-fd", -1, "")
	if err := flags.Parse([]string{"--offline", "--ready-fd", "3"}); err != nil {
		t.Fatalf("--offline consumed the following flag: %v", err)
	}
	if *readyFd != 3 {
		t.Errorf("--ready-fd is %d, want 3: --offline consumed its value", *readyFd)
	}
}

// A value that is not a bool never becomes a config.
//
// This is the regression: reading the flag back with flags.Visit and re-parsing
// its string, discarding the re-parse's error, left the config at false on a value
// the resolver could not read — and the network stayed open on the flag whose only
// job is closing it. Two things now prevent that state, and both are needed. The flag package parses the value, so an unreadable one is a parse
// error; and serve's flag set is ExitOnError, so a parse error has already printed
// why and exited before the config exists. The assertion below is the first of the
// two — the second is a property of the flag set, and it is why the discarded error
// in serve is safe rather than merely convenient.
func TestOfflineFlagRefusesAValueItCannotRead(t *testing.T) {
	cfg, err := parseOffline(t, "--offline=maybe")
	if err == nil {
		t.Fatal("--offline=maybe was accepted, so a typo in the flag is a silent decision about the network")
	}
	// The resolver did not treat the unreadable value as an answer either way. Had
	// it, an operator who mistyped the flag would get a server that silently ignores
	// it rather than one that tells them.
	if !cfg.Offline {
		t.Error("an unreadable --offline opened the network, and the only safe reading of a value nobody can parse is the one the environment already chose")
	}
}
