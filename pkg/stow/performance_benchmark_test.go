package stow_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"runtime"
	"sync/atomic"
	"testing"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func BenchmarkStorage(b *testing.B) {
	for _, backend := range []string{"memory", "filesystem"} {
		for _, size := range []int{4096, 1048576, 8388608} {
			for _, operation := range []string{"put", "get", "quarantine"} {
				b.Run(fmt.Sprintf("%s/%s/%dB", backend, operation, size), func(b *testing.B) {
					benchmarkStorage(b, backend, operation, size)
				})
			}
		}
	}
}

func benchmarkStorage(b *testing.B, backend, operation string, size int) {
	store := benchmarkRuntime(b, backend)
	ctx := context.Background()
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		b.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "benchmark", "seed", payload, stow.PutOptions{}); err != nil {
		b.Fatal(err)
	}
	expected := sha256.Sum256(payload)
	var workers atomic.Uint64
	writtenKeys := make(chan string, runtime.GOMAXPROCS(0))
	b.SetBytes(int64(size))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		key := fmt.Sprintf("worker-%d", workers.Add(1))
		body := bytes.Clone(payload)
		var sequence uint64
		written := false
		defer func() {
			if written && operation == "put" {
				writtenKeys <- key
			}
		}()
		for pb.Next() {
			if operation == "get" {
				if err := benchmarkRead(ctx, store, "seed", expected, size); err != nil {
					b.Error(err)
					return
				}
				continue
			}
			sequence++
			binary.LittleEndian.PutUint64(body, sequence)
			if _, err := store.PutObjectStream(ctx, "benchmark", key, bytes.NewReader(body), stow.StreamPutOptions{Size: int64(size)}); err != nil {
				b.Error(err)
				return
			}
			written = true
			if operation == "quarantine" {
				if err := benchmarkQuarantine(ctx, store, key, body); err != nil {
					b.Error(err)
					return
				}
			}
		}
	})
	b.StopTimer()
	close(writtenKeys)
	if operation == "put" {
		verifyBenchmarkWrites(b, store, writtenKeys, payload)
	}
}

func verifyBenchmarkWrites(b *testing.B, store *stow.Runtime, keys <-chan string, payload []byte) {
	for key := range keys {
		reader, _, err := store.OpenObject(context.Background(), "benchmark", key)
		if err != nil {
			b.Fatal(err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(data) != len(payload) || !bytes.Equal(data[8:], payload[8:]) {
			b.Fatal("final upload verification failed")
		}
	}
}

func benchmarkRuntime(b *testing.B, backend string) *stow.Runtime {
	var store *stow.Runtime
	var err error
	options := stow.Options{MaxBytes: 256 << 20, MaxObjects: 4096}
	if backend == "filesystem" {
		store, err = stow.OpenFilesystem(stow.FilesystemOptions{Dir: b.TempDir(), Options: options})
	} else {
		store, err = stow.Open(options)
	}
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := store.Close(); err != nil {
			b.Error(err)
		}
	})
	if err := store.CreateBucket(context.Background(), "benchmark"); err != nil {
		b.Fatal(err)
	}
	return store
}

func benchmarkRead(ctx context.Context, store *stow.Runtime, key string, expected [32]byte, size int) error {
	reader, _, err := store.OpenObject(ctx, "benchmark", key)
	if err != nil {
		return err
	}
	hash := sha256.New()
	count, readErr := io.Copy(hash, reader)
	closeErr := reader.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if count != int64(size) || !bytes.Equal(hash.Sum(nil), expected[:]) {
		return fmt.Errorf("download verification failed")
	}
	return nil
}

func benchmarkQuarantine(ctx context.Context, store *stow.Runtime, key string, body []byte) error {
	if err := benchmarkRead(ctx, store, key, sha256.Sum256(body), len(body)); err != nil {
		return err
	}
	if _, err := store.PutObject(ctx, "benchmark", key+".verdict", []byte("simulated-clean"), stow.PutOptions{}); err != nil {
		return err
	}
	if err := store.DeleteObject(ctx, "benchmark", key); err != nil {
		return err
	}
	return store.DeleteObject(ctx, "benchmark", key+".verdict")
}
