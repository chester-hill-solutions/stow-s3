package benchmark_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/spf13/afero"
)

type objectStore interface {
	put(string, []byte) error
	open(string) (io.ReadCloser, error)
	remove(string) error
	close() error
}

type stowStore struct{ runtime *stow.Runtime }

func (s stowStore) close() error { return s.runtime.Close() }

func (s stowStore) put(key string, body []byte) error {
	_, err := s.runtime.PutObjectStream(context.Background(), "benchmark", key, bytes.NewReader(body), stow.StreamPutOptions{Size: int64(len(body))})
	return err
}
func (s stowStore) open(key string) (io.ReadCloser, error) {
	r, _, err := s.runtime.OpenObject(context.Background(), "benchmark", key)
	return r, err
}
func (s stowStore) remove(key string) error {
	return s.runtime.DeleteObject(context.Background(), "benchmark", key)
}

type memoryStore struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

func (s *memoryStore) close() error { return nil }

func (s *memoryStore) put(key string, body []byte) error {
	owned := bytes.Clone(body)
	s.mu.Lock()
	s.objects[key] = owned
	s.mu.Unlock()
	return nil
}
func (s *memoryStore) open(key string) (io.ReadCloser, error) {
	s.mu.RLock()
	body, ok := s.objects[key]
	s.mu.RUnlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}
func (s *memoryStore) remove(key string) error {
	s.mu.Lock()
	delete(s.objects, key)
	s.mu.Unlock()
	return nil
}

type aferoStore struct {
	fs   afero.Fs
	root string
}

func (s aferoStore) close() error { return nil }

func (s aferoStore) put(key string, body []byte) error {
	return afero.WriteFile(s.fs, filepath.Join(s.root, key), body, 0600)
}
func (s aferoStore) open(key string) (io.ReadCloser, error) {
	return s.fs.Open(filepath.Join(s.root, key))
}
func (s aferoStore) remove(key string) error { return s.fs.Remove(filepath.Join(s.root, key)) }

type fileStore struct {
	root       string
	syncWrites bool
}

func (s fileStore) close() error { return nil }

func (s fileStore) put(key string, body []byte) error {
	file, err := os.CreateTemp(s.root, "stage-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(body); err != nil {
		file.Close()
		return err
	}
	if s.syncWrites {
		if err = file.Sync(); err != nil {
			file.Close()
			return err
		}
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(s.root, key)); err != nil {
		return err
	}
	if s.syncWrites {
		return syncDirectory(s.root)
	}
	return nil
}
func (s fileStore) open(key string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.root, key))
}
func (s fileStore) remove(key string) error {
	if err := os.Remove(filepath.Join(s.root, key)); err != nil {
		return err
	}
	if s.syncWrites {
		return syncDirectory(s.root)
	}
	return nil
}
func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func openStore(b *testing.B, implementation string) objectStore {
	b.Helper()
	store, err := newObjectStore(b.TempDir(), implementation)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := store.close(); err != nil {
			b.Error(err)
		}
	})
	if err := validateStoreConfig(store); err != nil {
		b.Fatal(err)
	}
	return store
}

func newObjectStore(root, implementation string) (objectStore, error) {
	if implementation == "buffer-memory" {
		return &memoryStore{objects: make(map[string][]byte)}, nil
	}
	if implementation == "afero-memory" {
		return aferoStore{fs: afero.NewMemMapFs()}, nil
	}
	if implementation == "afero-filesystem" {
		return aferoStore{fs: afero.NewOsFs(), root: root}, nil
	}
	if implementation == "tempfile-filesystem" {
		return fileStore{root: root}, nil
	}
	if implementation == "atomic-filesystem" {
		return fileStore{root: root, syncWrites: true}, nil
	}
	if implementation == "sqlite-memory" || implementation == "sqlite-filesystem" {
		return openSQLite(root, implementation == "sqlite-memory")
	}
	if implementation != "stow-memory" && implementation != "stow-filesystem" {
		return nil, fmt.Errorf("unknown implementation %s", implementation)
	}
	var r *stow.Runtime
	var err error
	options := stow.Options{MaxBytes: 256 << 20, MaxObjects: 4096}
	if implementation == "stow-memory" {
		r, err = stow.Open(options)
	} else {
		r, err = stow.OpenFilesystem(stow.FilesystemOptions{Dir: root, Options: options})
	}
	if err != nil {
		return nil, err
	}
	if err := r.CreateBucket(context.Background(), "benchmark"); err != nil {
		r.Close()
		return nil, err
	}
	return stowStore{runtime: r}, nil
}

var embeddedImplementations = []string{"buffer-memory", "afero-memory", "sqlite-memory", "stow-memory", "tempfile-filesystem", "atomic-filesystem", "afero-filesystem", "sqlite-filesystem", "stow-filesystem"}

func verifiedRead(store objectStore, key string, expected [32]byte, size int) error {
	r, err := store.open(key)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.Copy(hash, r)
	closeErr := r.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n != int64(size) || !bytes.Equal(hash.Sum(nil), expected[:]) {
		return fmt.Errorf("content verification failed")
	}
	return nil
}

func BenchmarkComparison(b *testing.B) {
	for _, implementation := range embeddedImplementations {
		for _, size := range []int{4096, 1048576, 8388608} {
			for _, operation := range []string{"put", "get", "quarantine"} {
				b.Run(fmt.Sprintf("%s/%s/%dB", implementation, operation, size), func(b *testing.B) {
					benchmarkStore(b, implementation, operation, size)
				})
			}
		}
	}
}

func benchmarkStore(b *testing.B, implementation, operation string, size int) {
	store := openStore(b, implementation)
	body := make([]byte, size)
	if _, err := rand.Read(body); err != nil {
		b.Fatal(err)
	}
	if err := store.put("seed", body); err != nil {
		b.Fatal(err)
	}
	expected := sha256.Sum256(body)
	var workers atomic.Uint64
	var final sync.Map
	b.SetBytes(int64(size))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		key := fmt.Sprintf("worker-%d", workers.Add(1))
		payload := bytes.Clone(body)
		var sequence uint64
		written := false
		for pb.Next() {
			if operation == "get" {
				if err := verifiedRead(store, "seed", expected, size); err != nil {
					b.Error(err)
					return
				}
				continue
			}
			sequence++
			binary.LittleEndian.PutUint64(payload, sequence)
			if err := store.put(key, payload); err != nil {
				b.Error(err)
				return
			}
			written = true
			if operation == "put" {
				continue
			}
			if err := quarantine(store, key, payload); err != nil {
				b.Error(err)
				return
			}
		}
		if operation == "put" && written {
			final.Store(key, bytes.Clone(payload))
		}
	})
	b.StopTimer()
	final.Range(func(key, payload any) bool {
		data := payload.([]byte)
		if err := verifiedRead(store, key.(string), sha256.Sum256(data), size); err != nil {
			b.Error(err)
		}
		return true
	})
}

func quarantine(store objectStore, key string, body []byte) error {
	if err := verifiedRead(store, key, sha256.Sum256(body), len(body)); err != nil {
		return err
	}
	if err := store.put(key+".verdict", []byte("simulated-clean")); err != nil {
		return err
	}
	if err := store.remove(key); err != nil {
		return err
	}
	return store.remove(key + ".verdict")
}

func TestMemoryOwnership(t *testing.T) {
	store := &memoryStore{objects: make(map[string][]byte)}
	body := []byte("original")
	expected := sha256.Sum256(body)
	if err := store.put("key", body); err != nil {
		t.Fatal(err)
	}
	body[0] = 'X'
	if err := verifiedRead(store, "key", expected, len(body)); err != nil {
		t.Fatal(err)
	}
}
