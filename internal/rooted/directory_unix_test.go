//go:build unix

package rooted

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOpenDirectoryRefusesFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	done := make(chan error, 1)
	go func() {
		file, err := root.OpenDirectory("pipe")
		if file != nil {
			_ = file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotDirectory) {
			t.Fatalf("OpenDirectory(FIFO) error = %v, want ErrNotDirectory", err)
		}
	case <-time.After(time.Second):
		t.Fatal("OpenDirectory(FIFO) blocked")
	}
}
