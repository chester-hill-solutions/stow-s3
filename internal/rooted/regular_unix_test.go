//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package rooted

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOpenRegularFileRefusesSpecialInputs(t *testing.T) {
	path := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(path, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "regular"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{"pipe", "."} {
		if file, err := root.OpenRegularFile(name); err == nil {
			file.Close()
			t.Fatalf("accepted %s", name)
		}
	}
	file, err := root.OpenRegularFile("regular")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
}

func TestOpenRegularFileReplacementCannotBlock(t *testing.T) {
	path := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(path, "replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	done := make(chan error, 1)
	go func() {
		file, err := openRegularFile(root, "replacement")
		if file != nil {
			file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("replacement FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("opening a replacement FIFO blocked")
	}
}
