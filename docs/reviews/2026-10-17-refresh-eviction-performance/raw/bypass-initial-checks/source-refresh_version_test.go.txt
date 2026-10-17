package runthrough_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestRefreshFallbackCapturesVersion(t *testing.T) {
	for _, mode := range []string{"adapter", "runtime"} {
		for _, body := range []string{"new", "longer new body", "", "old"} {
			for _, durable := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/body=%q/durable=%t", mode, body, durable), func(t *testing.T) {
					var outbox runthrough.Outbox
					if durable {
						outbox = fileVersionOutbox(t)
					}
					f := newVersionFixture(t, body, outbox)
					usage := f.instance.Usage()
					data, meta, err := observeVersion(f, mode)
					if err != nil {
						t.Fatalf("coherent fresh read failed: %v", err)
					}
					assertVersionResult(t, f, data, meta)
					assertEvictedVersion(t, f)
					if f.instance.Usage() != usage {
						t.Error("upstream read changed authoritative accounting")
					}
				})
			}
		}
	}
}
func TestRefreshFallbackProvenanceFailureClosesReader(t *testing.T) {
	for _, mode := range []string{"adapter", "runtime"} {
		t.Run(mode, func(t *testing.T) {
			failure := errors.New("cannot persist fresh observation")
			outbox := &versionOutbox{MemoryOutbox: runthrough.NewMemoryOutbox(), failure: failure}
			f := newVersionFixture(t, "new", outbox)
			_, _, err := observeVersion(f, mode)
			if err != failure {
				t.Errorf("fresh observation failure changed: got=%v want=%v", err, failure)
			}
			if f.client.closes != 2 {
				t.Errorf("fresh reader leak: closes=%d", f.client.closes)
			}
			outbox.failure = nil
			data, meta, err := observeVersion(f, mode)
			if err != nil {
				t.Fatal(err)
			}
			assertVersionResult(t, f, data, meta)
		})
	}
}
func TestRefreshFallbackRetainsStableVersion(t *testing.T) {
	outbox := &versionOutbox{MemoryOutbox: runthrough.NewMemoryOutbox()}
	f := newVersionFixture(t, "old", outbox)
	f.client.change = nil
	data, meta, err := observeVersion(f, "adapter")
	if err != nil {
		t.Fatal(err)
	}
	assertVersionResult(t, f, data, meta)
	assertEvictedVersion(t, f)
	if outbox.saves != 1 {
		t.Errorf("unchanged observation rewritten: saves=%d", outbox.saves)
	}
}
func TestRefreshFallbackMissingETagClosesReader(t *testing.T) {
	f := newVersionFixture(t, "new", nil)
	f.client.change = func() error {
		if f.client.gets != 2 {
			return nil
		}
		if err := versionPut(context.Background(), f.client.mockUpstream, "new"); err != nil {
			return err
		}
		f.client.mu.Lock()
		defer f.client.mu.Unlock()
		meta := f.client.objects[objectKey("probe", "target")]
		meta.ETag = ""
		f.client.objects[objectKey("probe", "target")] = meta
		return nil
	}
	reader, _, err := f.adapter.GetObject(context.Background(), "probe", "target")
	if err == nil || reader != nil {
		t.Errorf("unrecordable fresh version accepted: reader=%v error=%v", reader, err)
		if reader != nil {
			_ = reader.Close()
		}
	}
	if f.client.closes != 2 {
		t.Errorf("unrecordable fresh version leaked: closes=%d", f.client.closes)
	}
}
func TestRefreshHeadDoesNotRefetchEvictedBody(t *testing.T) {
	f := newVersionFixtureWithCache(t, "new", nil, nil)
	meta, err := f.adapter.HeadObject(context.Background(), "probe", "target")
	if err != nil || meta.ETag != f.original.ETag || f.client.gets != 1 || f.client.closes != 1 {
		t.Fatalf("head meta=%+v gets=%d closes=%d error=%v", meta, f.client.gets, f.client.closes, err)
	}
}

func TestRefreshFallbackDurableProvenanceSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.json")
	outbox, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	f := newVersionFixture(t, "new", outbox)
	data, meta, err := observeVersion(f, "adapter")
	if err != nil {
		t.Fatal(err)
	}
	assertVersionResult(t, f, data, meta)
	if err := f.instance.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	state, known, err := reopened.UpstreamState("probe", "target")
	if err != nil || !known || state.ETag != meta.ETag {
		t.Errorf("reopened stale observation: state=%+v known=%t error=%v want=%s", state, known, err, meta.ETag)
	}
}
func TestRefreshFallbackObservationAuthorizesCurrentWrite(t *testing.T) {
	f := newVersionFixture(t, "new", fileVersionOutbox(t))
	data, meta, err := observeVersion(f, "adapter")
	if err != nil {
		t.Fatal(err)
	}
	assertVersionResult(t, f, data, meta)
	mirror := runthrough.NewWithOutbox(runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true}, f.local, f.cache, f.client, f.outbox)
	if _, err := mirror.PutObject(context.Background(), "probe", "target", strings.NewReader("edit"), storage.PutOptions{}); err != nil {
		t.Fatalf("observed current version refused: %v", err)
	}
	reader, _, err := f.client.mockUpstream.GetObject(context.Background(), "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	written, err := io.ReadAll(reader)
	if err != nil || string(written) != "edit" {
		t.Fatalf("write=%q error=%v", written, err)
	}
}
