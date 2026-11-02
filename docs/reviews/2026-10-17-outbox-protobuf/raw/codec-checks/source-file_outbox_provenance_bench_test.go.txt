package runthrough

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func seedProvenanceBenchmark(b *testing.B, depth int, queued bool) (*FileOutbox, string) {
	b.Helper()
	path := filepath.Join(b.TempDir(), "outbox.json")
	outbox, err := NewFileOutbox(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := outbox.withState(func(s *outboxState) error {
		for i := 0; i < depth; i++ {
			s.provenance[outboxIdentity("bucket", fmt.Sprintf("seed-%06d", i))] = UpstreamState{ETag: "0123456789abcdef0123456789abcdef"}
		}
		if queued {
			for i := 0; i < depth; i++ {
				s.enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: fmt.Sprintf("pending-%06d", i), Version: "committed-version", UpstreamAbsent: true, CreatedAt: time.Unix(1700000000, 0).UTC()})
			}
		}
		s.provenance[outboxIdentity("bucket", "target")] = UpstreamState{ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	return outbox, path
}

func benchmarkProcessWrites(b *testing.B) uint64 {
	b.Helper()
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		b.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "wchar:") {
			n, err := strconv.ParseUint(strings.Fields(line)[1], 10, 64)
			if err != nil {
				b.Fatal(err)
			}
			return n
		}
	}
	b.Fatal("process write counter unavailable")
	return 0
}

func BenchmarkFileOutboxProvenance(b *testing.B) {
	for _, depth := range []int{0, 128, 2048, 16384} {
		for _, changed := range []bool{false, true} {
			mode := "unchanged"
			if changed {
				mode = "changed"
			}
			b.Run(fmt.Sprintf("provenance=%d/%s", depth, mode), func(b *testing.B) { benchmarkProvenanceSave(b, depth, false, changed) })
		}
	}
	for _, changed := range []bool{false, true} {
		mode := "unchanged"
		if changed {
			mode = "changed"
		}
		b.Run("mixed=1024/"+mode, func(b *testing.B) { benchmarkProvenanceSave(b, 1024, true, changed) })
	}
}

func benchmarkProvenanceSave(b *testing.B, depth int, queued, changed bool) {
	outbox, path := seedProvenanceBenchmark(b, depth, queued)
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	value := UpstreamState{ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	before := benchmarkProcessWrites(b)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if changed {
			if n%2 == 0 {
				value.ETag = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			} else {
				value.ETag = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
		}
		if err := outbox.SaveUpstreamState("bucket", "target", value); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	after := benchmarkProcessWrites(b)
	b.ReportMetric(float64(after-before)/float64(b.N), "written-B/op")
	b.ReportMetric(float64(info.Size()), "journal-B")
	got, known, err := outbox.UpstreamState("bucket", "target")
	if err != nil || !known || got != value {
		b.Fatalf("state=%+v known=%t error=%v", got, known, err)
	}
}
