package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The protected-path check is a pure predicate over paths, so it is tested
// directly rather than through a workspace. Testing it through Destroy would
// mean creating a workspace at a path that is someone's home or working
// directory, which is exactly what must never happen — and faking the store's
// root to reach it would test the fake, not the rule.

// TestProtectedReasonCatchesAncestors covers the case that a literal
// comparison misses: `dir: ".."` resolves to a path that is not the home
// directory but is an ancestor of it, and deleting that takes the home
// directory with it.
func TestProtectedReasonCatchesAncestors(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory on this host")
	}
	parent := filepath.Dir(strings.TrimRight(home, string(filepath.Separator)))
	if parent == home || parent == "/" {
		t.Skipf("home has no usable parent to test against: %q", home)
	}
	if reason, protected := protectedReason(parent); !protected {
		t.Errorf("an ancestor of the home directory was not protected")
	} else if !strings.Contains(reason, "home") {
		t.Errorf("reason = %q, want it to mention home", reason)
	}
}

// TestProtectedReasonProtectsWorkingDirectory covers the same rule for the
// process working directory.
func TestProtectedReasonProtectsWorkingDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Skipf("no working directory: %v", err)
	}
	if reason, protected := protectedReason(cwd); !protected {
		t.Errorf("the working directory itself was not protected")
	} else if !strings.Contains(reason, "working directory") {
		t.Errorf("reason = %q, want it to mention the working directory", reason)
	}
}

// TestProtectedReasonAllowsAnOrdinarySubdirectory is the case that keeps the
// backstop from being useless: a workspace in a temporary directory is the
// normal case and must be allowed, or every Destroy would refuse.
func TestProtectedReasonAllowsAnOrdinarySubdirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "workspace", "artifacts")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if reason, protected := protectedReason(nested); protected {
		t.Errorf("an ordinary workspace directory was protected as %q", reason)
	}
}

// TestProtectedReasonSeesThroughRelativePaths pins that the check resolves
// before comparing, so `..`, a trailing separator, and a relative path cannot
// be used to walk past it.
func TestProtectedReasonSeesThroughRelativePaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory on this host")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Skipf("no working directory: %v", err)
	}
	// Build the relative spelling from this host's actual working directory.
	// Assuming a fixed depth from t.TempDir made this resolve somewhere other
	// than home on platforms with a different temporary-directory layout.
	sneaky, err := filepath.Rel(cwd, home)
	if err != nil {
		t.Skipf("home and working directory cannot be related: %v", err)
	}
	resolved, err := filepath.Abs(sneaky)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(home) {
		t.Fatalf("relative fixture %q resolves to %q, want home %q", sneaky, resolved, home)
	}
	if _, protected := protectedReason(sneaky); !protected {
		t.Errorf("a relative path resolving to the home directory was not protected")
	}
	trailing := home + string(filepath.Separator)
	if _, protected := protectedReason(trailing); !protected {
		t.Errorf("a home directory with a trailing separator was not protected")
	}
}

// TestIsAncestorOrSelf covers the comparison itself, including the boundary case
// that a naive prefix test gets wrong: /home/alice must not be treated as an
// ancestor of /home/alice2.
func TestIsAncestorOrSelf(t *testing.T) {
	// The cases go through resolvePath, which is what both production call sites
	// do, and they are built from the platform's own separator. Hard-coded POSIX
	// literals were used here first and this test failed on windows-latest, where
	// "/a" is not a native path and the separator is not "/". The production code
	// was right: destroy.go always calls isAncestorOrSelf with resolvePath
	// results, and a function that stops deletion reaching an ancestor of the
	// working directory has no business being handed a path it cannot parse.
	sep := string(filepath.Separator)
	root := filepath.VolumeName(t.TempDir()) + sep
	a := filepath.Join(root, "a")
	ab := filepath.Join(a, "b")
	abc := filepath.Join(ab, "c")
	abSibling := filepath.Join(a, "bc")

	cases := []struct {
		candidate string
		protected string
		want      bool
		why       string
	}{
		{ab, ab, true, "a path is its own ancestor"},
		{a, ab, true, "a parent is an ancestor"},
		{ab, abSibling, false, "a shared prefix is not an ancestor"},
		{abSibling, ab, false, "a longer path is not an ancestor of a shorter one"},
		{abc, ab, false, "a descendant is not an ancestor"},
	}
	for _, c := range cases {
		if got := isAncestorOrSelf(resolvePath(c.candidate), resolvePath(c.protected)); got != c.want {
			t.Errorf("isAncestorOrSelf(%q, %q) = %v, want %v (%s)",
				c.candidate, c.protected, got, c.want, c.why)
		}
	}
}

// TestDirectoryExists distinguishes a directory from a file, because a
// workspace pointed at a regular file is a caller error rather than an
// adoption, and treating it as one would misreport ownership.
func TestDirectoryExists(t *testing.T) {
	dir := t.TempDir()
	if !directoryExists(dir) {
		t.Error("an existing directory was reported as absent")
	}
	file := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if directoryExists(file) {
		t.Error("a regular file was reported as a directory")
	}
	if directoryExists(filepath.Join(dir, "absent")) {
		t.Error("an absent path was reported as a directory")
	}
}
