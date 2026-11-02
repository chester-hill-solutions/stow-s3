package runthrough

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough/outboxpb"
	"google.golang.org/protobuf/proto"
)

// Fixtures are seeded by a separate process so encoder memory does not enter
// the benchmark process high-water RSS. Run this diagnostic on Linux.
func outboxReadCostFixtures() []struct {
	name   string
	depth  int
	queued bool
} {
	return []struct {
		name   string
		depth  int
		queued bool
	}{
		{"provenance=0", 0, false}, {"provenance=2048", 2048, false},
		{"provenance=16384", 16384, false}, {"provenance=65536", 65536, false},
		{"provenance=262144", 262144, false}, {"mixed=1024", 1024, true}, {"mixed=16384", 16384, true},
	}
}

func TestOutboxReadCostSeed(t *testing.T) {
	root := os.Getenv("STOW_OUTBOX_READ_COST_SEED")
	if root == "" {
		t.Skip("separate-process diagnostic fixture generator")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{}
	for _, fixture := range outboxReadCostFixtures() {
		path := filepath.Join(root, fixture.name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("fixture must be new: %s: %v", path, err)
		}
		outbox, err := NewFileOutbox(path)
		if err != nil {
			t.Fatal(err)
		}
		err = outbox.withState(func(s *outboxState) error {
			for i := 0; i < fixture.depth; i++ {
				s.provenance[outboxIdentity("bucket", fmt.Sprintf("seed-%06d", i))] = UpstreamState{ETag: "0123456789abcdef0123456789abcdef"}
				if fixture.queued {
					s.enqueue(OutboxEntry{Operation: OutboxPut, Bucket: "bucket", Key: fmt.Sprintf("pending-%06d", i), Version: "committed-version", UpstreamAbsent: true, CreatedAt: time.Unix(1700000000, 0).UTC()})
				}
			}
			s.provenance[outboxIdentity("bucket", "target")] = UpstreamState{ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		manifest[fixture.name] = map[string]any{"provenanceRows": fixture.depth + 1, "pendingRows": len(outbox.state.entries), "bytes": info.Size()}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkFileOutboxReadCost(b *testing.B) {
	root := os.Getenv("STOW_OUTBOX_READ_COST_FIXTURES")
	if root == "" {
		b.Skip("requires separately seeded diagnostic fixtures")
	}
	for _, fixture := range outboxReadCostFixtures() {
		b.Run(fixture.name, func(b *testing.B) {
			for _, stage := range []string{"read", "unmarshal", "decode", "lookup", "unchanged", "changed"} {
				b.Run(stage, func(b *testing.B) {
					benchmarkOutboxReadCost(b, filepath.Join(root, fixture.name), fixture.depth, fixture.queued, stage)
				})
			}
		})
	}
}

func benchmarkOutboxReadCost(b *testing.B, path string, depth int, queued bool, stage string) {
	b.Helper()
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	switch stage {
	case "read":
		var data []byte
		measureOutboxReadCost(b, func(int) error { next, err := os.ReadFile(path); runtime.KeepAlive(data); data = next; return err })
		if int64(len(data)) != info.Size() {
			b.Fatal("read length changed")
		}
		runtime.KeepAlive(data)
	case "unmarshal", "decode":
		benchmarkOutboxDecodeCost(b, path, depth, queued, stage)
	default:
		benchmarkOutboxAPICost(b, path, stage)
	}
	b.ReportMetric(float64(info.Size()), "journal-B")
}

func measureOutboxReadCost(b *testing.B, operation func(int) error) {
	b.Helper()
	runtime.GC()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if err := operation(n); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(outboxReadCostHWM(b)), "process-HWM-B")
}

func benchmarkOutboxDecodeCost(b *testing.B, path string, depth int, queued bool, stage string) {
	b.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := decodeOutboxState(data); err != nil {
		b.Fatal(err)
	}
	var decoded outboxState
	var message *outboxpb.Outbox
	measureOutboxReadCost(b, func(int) error {
		if stage == "unmarshal" {
			next := &outboxpb.Outbox{}
			err := proto.Unmarshal(data[outboxProtoPrefixSize:], next)
			runtime.KeepAlive(message)
			message = next
			return err
		}
		next, err := decodeOutboxState(data)
		runtime.KeepAlive(decoded)
		decoded = next
		return err
	})
	if stage == "unmarshal" {
		if len(message.Provenance) != depth+1 {
			b.Fatal("unmarshal lost provenance")
		}
	} else if len(decoded.provenance) != depth+1 || (queued && len(decoded.entries) != depth) {
		b.Fatal("decode lost rows")
	}
	runtime.KeepAlive(data)
	runtime.KeepAlive(decoded)
	runtime.KeepAlive(message)
}

func benchmarkOutboxAPICost(b *testing.B, path, stage string) {
	b.Helper()
	outbox, err := NewFileOutbox(path)
	if err != nil {
		b.Fatal(err)
	}
	value := UpstreamState{ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if err := outbox.SaveUpstreamState("bucket", "target", value); err != nil {
		b.Fatal(err)
	}
	var got UpstreamState
	var known bool
	measureOutboxReadCost(b, func(n int) error {
		if stage == "lookup" {
			var err error
			got, known, err = outbox.UpstreamState("bucket", "target")
			return err
		}
		if stage == "changed" {
			if n%2 == 0 {
				value.ETag = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			} else {
				value.ETag = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
		}
		return outbox.SaveUpstreamState("bucket", "target", value)
	})
	if stage == "lookup" {
		if !known || got != value {
			b.Fatalf("lookup: %+v %t", got, known)
		}
	} else {
		actual, exists, err := outbox.UpstreamState("bucket", "target")
		if err != nil || !exists || actual != value {
			b.Fatalf("save: %+v %t %v", actual, exists, err)
		}
	}
	runtime.KeepAlive(outbox)
}

func outboxReadCostHWM(b *testing.B) uint64 {
	b.Helper()
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		b.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmHWM:") {
			n, err := strconv.ParseUint(strings.Fields(line)[1], 10, 64)
			if err != nil {
				b.Fatal(err)
			}
			return n * 1024
		}
	}
	b.Fatal("process high-water RSS unavailable")
	return 0
}
