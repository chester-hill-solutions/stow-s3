package fs_test

import (
	"bufio"
	"os"
	"os/exec"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

// The store lock is the process's ownership of its directory, and exactly one
// process may own it at a time. Before the OS lock this could be violated: a
// crashed owner left a lock file behind, and recovery checked staleness and then
// removed the path in a separate step, so two processes that both decided an old
// lock was stale could each remove and recreate it and both believe they owned
// the directory.
func TestStoreLockAdmitsOneOwnerAtATime(t *testing.T) {
	dir := t.TempDir()
	first, err := fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatalf("first store: %v", err)
	}
	if _, err := fs.NewFilesystemStore(dir); err == nil {
		t.Fatal("a second store acquired a directory the first owns")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}
	second, err := fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatalf("store after the owner closed: %v", err)
	}
	_ = second.Close()
}

// The same exclusion across processes, which is what the single-owner property
// actually protects, plus the property that a dead owner leaves no stale lock:
// the kernel releases the lock when the process exits.
func TestStoreLockExcludesAnotherProcess(t *testing.T) {
	if os.Getenv("STOW_STORE_LOCK_CHILD") == "1" {
		if _, err := fs.NewFilesystemStore(os.Getenv("STOW_STORE_LOCK_DIR")); err != nil {
			os.Exit(3)
		}
		_, _ = os.Stdout.WriteString("held\n")
		select {} // hold the lock until the parent kills this process
	}

	dir := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=TestStoreLockExcludesAnotherProcess")
	child.Env = append(os.Environ(), "STOW_STORE_LOCK_CHILD=1", "STOW_STORE_LOCK_DIR="+dir)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatalf("child stdout: %v", err)
	}
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	stop := func() { _ = child.Process.Kill(); _ = child.Wait() }
	defer stop()

	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "held\n" {
		t.Fatalf("child did not take the lock: %q, %v", line, err)
	}
	if _, err := fs.NewFilesystemStore(dir); err == nil {
		t.Fatal("this process acquired a store a child process owns")
	}
	stop()
	// The kernel released the lock when the child died, so the directory is
	// immediately available with no stale-lock recovery.
	store, err := fs.NewFilesystemStore(dir)
	if err != nil {
		t.Fatalf("store after the owner died: %v", err)
	}
	_ = store.Close()
}
