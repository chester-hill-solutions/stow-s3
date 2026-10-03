package rooted

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMutationsStayConfinedDuringAncestorSwaps(t *testing.T) {
	if !Supported() {
		t.Skip("safe rooted operations unavailable")
	}
	path, outside := t.TempDir(), t.TempDir()
	root, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	parent, parked := filepath.Join(path, "parent"), filepath.Join(path, "parked")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "target")
	if err := os.WriteFile(sentinel, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if err := os.Rename(parent, parked); err != nil {
				continue
			}
			if err := os.Symlink(outside, parent); err == nil {
				_ = os.Remove(parent)
			}
			_ = os.Rename(parked, parent)
		}
	}()
	for i := 0; i < 200; i++ {
		_ = root.WriteAtomic("parent/target", []byte("inside"), 0600)
		_ = root.Remove("parent/target")
	}
	wg.Wait()
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "outside" {
		t.Fatalf("outside mutation during race: %q %v", data, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 1 {
		t.Fatalf("outside temp files created: %+v %v", entries, err)
	}
}
