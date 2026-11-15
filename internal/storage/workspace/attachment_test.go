package workspace

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func attachmentFixture(t *testing.T) (*Registry, string, string) {
	t.Helper()
	registryDir := filepath.Join(t.TempDir(), "registry")
	registry, err := OpenRegistry(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "workspace")
	store, err := New(Options{Root: root, Bucket: "bucket", TTLSeconds: 1, InitiallyOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	const id = "ws-attachment"
	if err := registry.Register(Entry{ID: id, Dir: root, Bucket: "bucket", Owned: true, TTLSeconds: 1}); err != nil {
		t.Fatal(err)
	}
	return registry, registryDir, root
}

func requireAttachmentLocks(t *testing.T) {
	t.Helper()
	if !LockSupported() {
		t.Skip("workspace advisory locks are unsupported on this host")
	}
}

func TestReadAttachmentsShareLeaseAndCoexistWithWriterSession(t *testing.T) {
	requireAttachmentLocks(t)
	_, registryDir, root := attachmentFixture(t)
	first, err := AcquireReadAttachment(registryDir, "ws-attachment")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := AcquireReadAttachment(registryDir, "ws-attachment")
	if err != nil {
		t.Fatalf("second shared attachment: %v", err)
	}
	defer second.Release()
	session, err := AcquireSession(root)
	if err != nil {
		t.Fatalf("writer session should coexist with a read lease: %v", err)
	}
	defer session.Release()
}

func TestRemoveWorkspaceRefusesReadAttachmentWithoutCallingRemoval(t *testing.T) {
	requireAttachmentLocks(t)
	registry, registryDir, _ := attachmentFixture(t)
	lease, err := AcquireReadAttachment(registryDir, "ws-attachment")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	called := false
	err = registry.RemoveWorkspace("ws-attachment", func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrWorkspaceInUse) {
		t.Fatalf("RemoveWorkspace error = %v, want ErrWorkspaceInUse", err)
	}
	if called {
		t.Fatal("removal callback ran while a read attachment was held")
	}
	if _, found, err := registry.Lookup("ws-attachment"); err != nil || !found {
		t.Fatalf("workspace identity after refused removal: found=%v err=%v", found, err)
	}
}

func TestRemoveWorkspaceSucceedsAfterAttachmentRelease(t *testing.T) {
	requireAttachmentLocks(t)
	registry, registryDir, _ := attachmentFixture(t)
	lease, err := AcquireReadAttachment(registryDir, "ws-attachment")
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := registry.RemoveWorkspace("ws-attachment", func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("RemoveWorkspace after release: %v", err)
	}
	if !called {
		t.Fatal("removal callback was not called")
	}
	if _, found, err := registry.Lookup("ws-attachment"); err != nil || found {
		t.Fatalf("workspace identity after removal: found=%v err=%v", found, err)
	}
}

func TestCollectKeepsExpiredWorkspaceWithReadAttachment(t *testing.T) {
	requireAttachmentLocks(t)
	registry, registryDir, root := attachmentFixture(t)
	lease, err := AcquireReadAttachment(registryDir, "ws-attachment")
	if err != nil {
		t.Fatal(err)
	}
	results, err := registry.Collect(time.Now().Add(2 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Reason != "in-use" {
		t.Fatalf("collect result = %+v, want in-use refusal", results)
	}
	if _, found, err := registry.Lookup("ws-attachment"); err != nil || !found {
		t.Fatalf("reader attachment was collected: found=%v err=%v", found, err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	results, err = registry.Collect(time.Now().Add(2 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Reason != "expired" {
		t.Fatalf("collect after reader close = %+v, want expired", results)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired workspace survived after reader close: %v", err)
	}
	if _, found, err := registry.Lookup("ws-attachment"); err != nil || found {
		t.Fatalf("workspace identity survived collection: found=%v err=%v", found, err)
	}
}

func TestReadAttachmentHelperProcess(t *testing.T) {
	if os.Getenv("STOW_ATTACHMENT_HELPER") != "1" {
		return
	}
	lease, err := AcquireReadAttachment(os.Getenv("STOW_ATTACHMENT_REGISTRY"), "ws-attachment")
	if err != nil {
		t.Fatal(err)
	}
	_ = lease
	if err := os.WriteFile(os.Getenv("STOW_ATTACHMENT_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {}
}

func TestReadAttachmentLeaseIsReleasedWhenProcessIsKilled(t *testing.T) {
	requireAttachmentLocks(t)
	_, registryDir, _ := attachmentFixture(t)
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestReadAttachmentHelperProcess$")
	cmd.Env = append(os.Environ(),
		"STOW_ATTACHMENT_HELPER=1",
		"STOW_ATTACHMENT_REGISTRY="+registryDir,
		"STOW_ATTACHMENT_READY="+ready,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper process did not acquire its attachment lease")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed helper process unexpectedly exited successfully")
	}
	cmd.Process = nil
	lock, err := acquireAttachmentRemoval(registryDir, "ws-attachment")
	if err != nil {
		t.Fatalf("removal lease after killed reader: %v", err)
	}
	_ = lock.Release()
}

func TestAttachmentLocksRefuseOnUnsupportedHost(t *testing.T) {
	if LockSupported() {
		t.Skip("host supports advisory locks")
	}
	registryDir := filepath.Join(t.TempDir(), "registry")
	if _, err := AcquireReadAttachment(registryDir, "ws-attachment"); !errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("AcquireReadAttachment error = %v, want ErrLockUnsupported", err)
	}
	if _, err := acquireAttachmentRemoval(registryDir, "ws-attachment"); !errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("acquireAttachmentRemoval error = %v, want ErrLockUnsupported", err)
	}
}

func TestAttachmentLocksRejectEmptyRegistryWithoutCreatingFiles(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if _, err := AcquireReadAttachment("", "ws-attachment"); err == nil {
		t.Fatal("empty registry unexpectedly acquired a read attachment")
	}
	if _, err := acquireAttachmentRemoval("", "ws-attachment"); err == nil {
		t.Fatal("empty registry unexpectedly acquired a removal lease")
	}
	if _, err := os.Stat(filepath.Join(dir, ".stow")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty registry created a .stow directory: %v", err)
	}
}
