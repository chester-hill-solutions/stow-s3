package runthrough_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestExplicitUpstreamScopeSurvivesLaterLocalBucketCreation(t *testing.T) {
	ctx := context.Background()
	remote := storage.NewMemoryStore()
	for _, bucket := range []string{"allowed", "blocked"} {
		if err := remote.CreateBucket(ctx, bucket); err != nil {
			t.Fatal(err)
		}
		if _, err := remote.PutObject(ctx, bucket, "remote", bytes.NewBufferString("remote-value"), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	server, err := s3api.New(s3api.Config{Store: remote, Auth: s3api.DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	var allowedCalls, blockedCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/blocked") {
			blockedCalls.Add(1)
		} else {
			allowedCalls.Add(1)
		}
		server.Handler().ServeHTTP(w, r)
	}))
	defer upstream.Close()
	client, err := runthrough.NewS3Client(runthrough.UpstreamConfig{Endpoint: upstream.URL, AccessKey: "scope-test", SecretKey: "scope-secret"})
	if err != nil {
		t.Fatal(err)
	}
	local := storage.NewMemoryStore()
	if err := local.CreateBucket(ctx, "allowed"); err != nil {
		t.Fatal(err)
	}
	outbox, err := runthrough.NewFileOutbox(filepath.Join(t.TempDir(), "outbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	config := runthrough.Config{Policy: runthrough.PolicyMirrorWrites, AllowLiveWrites: true, Upstream: runthrough.UpstreamConfig{Bucket: "allowed"}}
	adapter := runthrough.NewWithOutbox(config, local, storage.NewMemoryStore(), client, outbox)
	defer adapter.Close()
	config.Upstream.Bucket = ""
	returned := adapter.Config()
	returned.Upstream.Bucket = "blocked"
	if err := adapter.CreateBucket(ctx, "blocked"); err != nil {
		t.Fatal(err)
	}
	assertBlockedBucketStaysLocal(t, adapter)
	body, _, err := adapter.GetObject(ctx, "allowed", "remote")
	if err != nil {
		t.Fatal(err)
	}
	_ = body.Close()
	if allowedCalls.Load() == 0 || blockedCalls.Load() != 0 {
		t.Fatalf("upstream requests allowed=%d blocked=%d", allowedCalls.Load(), blockedCalls.Load())
	}
	assertBlockedRetryStaysLocal(t, adapter, outbox)
	if blockedCalls.Load() != 0 {
		t.Fatal("retry escaped upstream bucket scope")
	}
}

func assertBlockedBucketStaysLocal(t *testing.T, adapter *runthrough.Adapter) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := adapter.GetObject(ctx, "blocked", "remote"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("blocked GET=%v", err)
	}
	if _, err := adapter.HeadObject(ctx, "blocked", "remote"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("blocked HEAD=%v", err)
	}
	listed, err := adapter.ListObjectsV2(ctx, "blocked", storage.ListOptions{})
	if err != nil || len(listed.Objects) != 0 {
		t.Fatalf("blocked LIST=%+v %v", listed, err)
	}
	if _, err := adapter.PutObject(ctx, "blocked", "local", bytes.NewBufferString("local"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.DeleteObject(ctx, "blocked", "local"); err != nil {
		t.Fatal(err)
	}
}

func assertBlockedRetryStaysLocal(t *testing.T, adapter *runthrough.Adapter, outbox *runthrough.FileOutbox) {
	t.Helper()
	ctx := context.Background()
	meta, err := adapter.PutObject(ctx, "blocked", "pending", bytes.NewBufferString("local"), storage.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.Enqueue(runthrough.OutboxEntry{Operation: runthrough.OutboxPut, Bucket: "blocked", Key: "pending", Version: meta.VersionID, UpstreamAbsent: true}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RetryPending(ctx); err != nil {
		t.Fatal(err)
	}
}
