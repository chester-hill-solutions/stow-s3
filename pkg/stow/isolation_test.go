package stow_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// TestMain redirects the config directory before any test runs.
//
// It exists because the package's tests were writing into the developer's real
// workspace registry. A test that calls PrepareWorkspace without a RegistryDir
// resolves the default one, and the default one is os.UserConfigDir() — so the
// developer's own ~/.config/stow-s3/workspaces. Nothing here noticed for a long
// time, and the evidence was sitting in plain sight on any machine that had run
// the suite: `workspace list` returning well over a thousand entries, 99% of them
// unreadable, every single one pointing at a /tmp directory a t.TempDir() had
// already removed.
//
// Two things make this the right place to fix rather than the call sites. The
// conformance driver already learned this lesson for its child process — it sets
// XDG_CONFIG_HOME and HOME to a temporary home for exactly this reason — and the Go
// unit tests simply never got the same treatment. And redirecting the directory
// once, here, covers the thirteen call sites that leak today and every one written
// tomorrow, where patching thirteen call sites covers thirteen call sites and
// leaves the fourteenth to be discovered the same way.
//
// Redirecting the config directory rather than passing a RegistryDir everywhere is
// also the more honest fix: it exercises the default path a caller with no
// arguments actually takes, so a test that means to check default resolution still
// checks it — against a temporary directory.
// developerConfigDir is where the config directory pointed before TestMain moved it.
//
// It has to be captured there rather than read in a test, because the redirect is
// the whole point: after it, os.UserConfigDir() correctly reports the temporary
// directory, so a test that compared against it would be comparing the answer with
// itself and always pass.
var developerConfigDir string

func TestMain(m *testing.M) {
	if dir, err := os.UserConfigDir(); err == nil {
		developerConfigDir = dir
	}
	// os.UserConfigDir reads XDG_CONFIG_HOME on Linux and falls back to
	// $HOME/.config, so both are set: a machine with either one set would otherwise
	// resolve somewhere this does not control.
	root, err := os.MkdirTemp("", "stow-pkg-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot create a temporary config directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "cannot create the temporary config directory: %v\n", err)
		os.RemoveAll(root)
		os.Exit(1)
	}
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	os.Setenv("HOME", filepath.Join(root, "home"))

	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}

// TestTheDefaultRegistryIsNotTheDevelopersOwn is the guard for the redirect above.
//
// Without it, removing TestMain is a silent change: the tests keep passing, the
// leak comes back, and the evidence is another thousand unreadable entries in a
// registry the developer did not know was in use. Both halves are asserted — that
// the default no longer resolves to the machine's own config directory, and that it
// does resolve inside the temporary one, because a redirect that stopped taking
// effect would otherwise satisfy the first half alone.
func TestTheDefaultRegistryIsNotTheDevelopersOwn(t *testing.T) {
	resolved, err := stow.ResolveRegistryDir("", "")
	if err != nil {
		t.Fatalf("resolve the default registry: %v", err)
	}
	if developerConfigDir == "" {
		t.Skip("the config directory could not be located before the redirect, so there is nothing to compare against")
	}
	if isUnder(resolved, developerConfigDir) {
		t.Errorf("the default registry resolved to %s, which is inside the machine's own config directory (%s): a test that prepares a workspace without a RegistryDir writes there, and did",
			resolved, developerConfigDir)
	}
	current, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("locate the redirected config directory: %v", err)
	}
	if !isUnder(current, os.TempDir()) {
		t.Errorf("the config directory is %s, which is not temporary: the redirect is not in effect, so the assertion above is passing for the wrong reason", current)
	}
}

// isUnder reports whether path is strictly beneath root. It compares a cleaned
// prefix rather than using strings.HasPrefix, because /a/bc is not under /a/b.
func isUnder(path, root string) bool {
	cleaned := filepath.Clean(path)
	if cleaned == filepath.Clean(root) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), cleaned)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && rel[0] != '.'
}
