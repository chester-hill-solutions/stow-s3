//go:build unix

package stow_test

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestWorkspaceReaderRefusesFIFOWithoutBlocking(t *testing.T) {
	ws, registryDir := workspaceReaderFixture(t)
	fifo := filepath.Join(ws.Dir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := stow.OpenWorkspaceReader(stow.WorkspaceReaderOptions{RegistryDir: registryDir, WorkspaceID: ws.ID()})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	assertReaderOperationReturns(t, func() error {
		_, err := reader.ReadFile(context.Background(), "pipe")
		return err
	})
	assertReaderOperationReturns(t, func() error {
		_, err := reader.ReadDirectory(context.Background(), "pipe")
		return err
	})
	entries, err := reader.ReadDirectory(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name == "pipe" {
			t.Fatal("directory listed FIFO")
		}
	}
}

func assertReaderOperationReturns(t *testing.T, operation func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- operation() }()
	select {
	case err := <-done:
		if !errors.Is(err, stow.ErrWorkspaceReadUnavailable) {
			t.Fatalf("special-file operation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("special-file operation blocked")
	}
}
