package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func performanceIndex(path string, count int) *Index {
	index := &Index{Version: manifestVersion, Buckets: map[string]map[string]ManifestEntry{"assets": {}}, path: path}
	for n := 0; n < count; n++ {
		index.setEntry("assets", fmt.Sprintf("files/%06d.txt", n), ManifestEntry{Form: FormNatural, Size: 4096, ETag: "\"9b0ac14990b8114352192d9ee0b1b8f2\"", VersionID: "version", ContentType: "text/plain", Metadata: map[string]string{"owner": "agent", "state": "saved"}, Modified: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)})
	}
	return index
}

func TestCompactIndexRetainsExistingLayoutAndMetadata(t *testing.T) {
	index := performanceIndex(filepath.Join(t.TempDir(), "index.json"), 3)
	if err := index.save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(index.path)
	if err != nil {
		t.Fatal(err)
	}
	var reopened Index
	if err := json.Unmarshal(data, &reopened); err != nil {
		t.Fatal(err)
	}
	if reopened.Version != manifestVersion || !reflect.DeepEqual(reopened.Buckets, index.Buckets) {
		t.Fatal("index round trip changed metadata")
	}
	legacy, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(index.path, legacy, 0644); err != nil {
		t.Fatal(err)
	}
	var previous Index
	if err := loadDocument(index.path, &previous); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(previous.Buckets, index.Buckets) {
		t.Fatal("indented index no longer readable")
	}
}

func BenchmarkWorkspaceIndex(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("%d", count), func(b *testing.B) {
			index := performanceIndex(filepath.Join(b.TempDir(), "index.json"), count)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if err := index.save(); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			info, err := os.Stat(index.path)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(info.Size()), "file-bytes")
		})
	}
}
