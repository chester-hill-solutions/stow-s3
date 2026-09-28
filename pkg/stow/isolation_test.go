package stow_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

// TestMain redirects the config directory before any test runs, so that a test
// which resolves the default registry writes to a temporary one rather than to the
// developer's own ~/.config.
//
// Redirecting once here rather than passing a RegistryDir at each call site covers
// the thirteen that leak today and every one written tomorrow, and it still
// exercises the default path a caller with no arguments takes.
// developerConfigDir is where the config directory pointed before TestMain moved it.
// Captured there rather than read in a test, which would be comparing the answer
// with itself.
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

// TestTheDefaultRegistryIsNotTheDevelopersOwn is the guard for the redirect. Both
// halves matter: a redirect that stopped taking effect would satisfy the first
// alone.
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

// isUnder compares a cleaned relative path rather than using strings.HasPrefix,
// because /a/bc is not under /a/b.
func isUnder(path, root string) bool {
	cleaned := filepath.Clean(path)
	if cleaned == filepath.Clean(root) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), cleaned)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && rel[0] != '.'
}
