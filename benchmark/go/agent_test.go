package benchmark_test

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type agentSample struct {
	Implementation string  `json:"implementation"`
	Bytes          int     `json:"bytes"`
	Sample         int     `json:"sample"`
	StartupMs      float64 `json:"startupMs"`
	FirstWriteMs   float64 `json:"firstWriteMs"`
	Verified       bool    `json:"verified"`
	SQLiteVersion  string  `json:"sqliteVersion,omitempty"`
}

func BenchmarkAgentStartup(b *testing.B) {
	for _, implementation := range embeddedImplementations {
		for _, size := range []int{4096, 1048576} {
			b.Run(fmt.Sprintf("%s/%dB", implementation, size), func(b *testing.B) {
				benchmarkAgentStartup(b, implementation, size)
			})
		}
	}
}

func benchmarkAgentStartup(b *testing.B, implementation string, size int) {
	root := b.TempDir()
	body := make([]byte, size)
	if _, err := rand.Read(body); err != nil {
		b.Fatal(err)
	}
	expected := sha256.Sum256(body)
	var startup, firstWrite time.Duration
	for index := 0; index < b.N; index++ {
		directory := filepath.Join(root, fmt.Sprintf("sample-%d", index))
		if err := os.Mkdir(directory, 0700); err != nil {
			b.Fatal(err)
		}
		began := time.Now()
		store, err := newObjectStore(directory, implementation)
		if err != nil {
			b.Fatal(err)
		}
		ready := time.Since(began)
		if err := store.put("first", body); err != nil {
			store.close()
			b.Fatal(err)
		}
		written := time.Since(began)
		startup += ready
		firstWrite += written
		err = verifiedRead(store, "first", expected, size)
		if err == nil {
			err = validateStoreConfig(store)
		}
		closeErr := store.close()
		if err != nil || closeErr != nil {
			b.Fatalf("first-write verification: %v, close: %v", err, closeErr)
		}
		if err := os.RemoveAll(directory); err != nil {
			b.Fatal(err)
		}
		if b.N > 1 {
			sample := agentSample{Implementation: implementation, Bytes: size, Sample: index,
				StartupMs: float64(ready) / float64(time.Millisecond), FirstWriteMs: float64(written) / float64(time.Millisecond), Verified: true}
			if strings.HasPrefix(implementation, "sqlite-") {
				sample.SQLiteVersion = sqliteEngineVersion()
			}
			data, err := json.Marshal(sample)
			if err != nil {
				b.Fatal(err)
			}
			fmt.Printf("\nAGENT_SAMPLE %s\n", data)
		}
	}
	b.ReportMetric(float64(startup.Nanoseconds())/float64(b.N), "startup-ns/op")
	b.ReportMetric(float64(firstWrite.Nanoseconds())/float64(b.N), "first-write-ns/op")
}
