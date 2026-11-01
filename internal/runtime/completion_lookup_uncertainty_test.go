package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type completionLookupFaultStore struct {
	storage.MultipartStore
	completed bool
	lookupErr error
	lookups   int
}

func (s *completionLookupFaultStore) CompleteMultipartUpload(ctx context.Context, id string, parts []storage.PartInfo) (*storage.ObjectMeta, error) {
	meta, err := s.MultipartStore.CompleteMultipartUpload(ctx, id, parts)
	s.completed = true
	return meta, err
}

func (s *completionLookupFaultStore) GetMultipartUpload(ctx context.Context, id string) (*storage.MultipartUpload, error) {
	if s.completed {
		s.lookups++
		if s.lookupErr != nil {
			return nil, s.lookupErr
		}
	}
	return s.MultipartStore.GetMultipartUpload(ctx, id)
}

func ambiguousCompletionLookup(order string) error {
	unavailable := errors.New("upload observation unavailable")
	if strings.Contains(order, "missing-first") {
		return errors.Join(storage.ErrUploadNotFound, unavailable, storage.ErrMutationCommitted)
	}
	return errors.Join(storage.ErrMutationCommitted, storage.ErrUploadNotFound, unavailable)
}

func TestCompletionLookupWorkspaceAmbiguity(t *testing.T) {
	for _, order := range []string{"marker-first", "missing-first", "wrapped-marker-first", "wrapped-missing-first"} {
		t.Run(order, func(t *testing.T) {
			f := newCompletionPublicationFixture(t)
			f.block(t, "cleanup")
			lookupErr := ambiguousCompletionLookup(order)
			if strings.HasPrefix(order, "wrapped-") {
				lookupErr = fmt.Errorf("lookup: %w", lookupErr)
			}
			wrapped := &completionLookupFaultStore{MultipartStore: f.instance.multipartStore, lookupErr: lookupErr}
			f.instance.multipartStore = wrapped
			meta, err := f.instance.CompleteMultipartUpload(context.Background(), f.upload, []storage.PartInfo{*f.part})
			if !errors.Is(err, storage.ErrMutationCommitted) || !errors.Is(err, lookupErr) {
				t.Errorf("completion lost committed/lookup error: %v", err)
			}
			assertCompletionLookupRetained(t, f, wrapped, meta)
			recoverCompletionLookup(t, f, wrapped)
		})
	}
}

func assertCompletionLookupRetained(t *testing.T, f *completionPublicationFixture, wrapped *completionLookupFaultStore, meta *storage.ObjectMeta) {
	t.Helper()
	if meta == nil || meta.Size != 4 || meta.VersionID == "" || meta.ETag != storage.CompletionETag([]string{f.part.ETag}) {
		t.Fatalf("committed identity=%+v", meta)
	}
	assertLookupPublishedObject(t, f.instance, meta)
	parts, err := wrapped.MultipartStore.ListParts(context.Background(), f.upload)
	if err != nil || len(parts) != 1 || parts[0].Size != 4 || parts[0].ETag != f.part.ETag {
		t.Fatalf("actual retained parts=%+v error=%v", parts, err)
	}
	if f.instance.Usage() != (Usage{Bytes: 4, Objects: 1}) || f.instance.reservedBytes != 4 || f.instance.reservedObjects != 0 || len(f.instance.multipart) != 1 {
		t.Errorf("retained usage=%+v bytes=%d objects=%d sessions=%d", f.instance.Usage(), f.instance.reservedBytes, f.instance.reservedObjects, len(f.instance.multipart))
	}
	if wrapped.lookups != 1 {
		t.Errorf("post-completion lookups=%d want one", wrapped.lookups)
	}
}

func assertLookupPublishedObject(t *testing.T, instance *Instance, meta *storage.ObjectMeta) {
	t.Helper()
	object, err := instance.GetObject(context.Background(), "probe", "target")
	if err != nil || string(object.Data) != "body" || object.VersionID != meta.VersionID || object.ETag != meta.ETag || object.ContentType != "text/plain" || object.Metadata["state"] != "retained" {
		t.Fatalf("committed object=%+v error=%v", object, err)
	}
}

func completionTargetPolicy() *policy.Set {
	var set policy.Set
	set.Add("runtime", "probe", policy.Selector{Kind: policy.KindObject, Prefix: "target"}, policy.Allow, authority.ObjectRead, authority.ObjectWrite)
	return &set
}

func recoverCompletionLookup(t *testing.T, f *completionPublicationFixture, wrapped *completionLookupFaultStore) {
	t.Helper()
	ctx := context.Background()
	before, err := f.instance.GetObject(ctx, "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	f.repair()
	wrapped.completed = false
	f.instance.policy = policy.Fixed(completionTargetPolicy())
	defer func() { f.instance.policy = nil }()
	if err := f.instance.AbortMultipartUpload(ctx, f.upload); err != nil {
		t.Errorf("repaired policy-scoped abort: %v", err)
		return
	}
	after, err := f.instance.GetObject(ctx, "probe", "target")
	if err != nil || string(after.Data) != string(before.Data) || after.VersionID != before.VersionID || after.ETag != before.ETag {
		t.Fatalf("abort changed committed object: %+v error=%v", after, err)
	}
	if f.instance.reservedBytes != 0 || len(f.instance.multipart) != 0 {
		t.Fatalf("abort retained bytes=%d sessions=%d", f.instance.reservedBytes, len(f.instance.multipart))
	}
	if _, err := f.instance.PutObject(ctx, "probe", "target", []byte("12345678"), PutOptions{}); err != nil {
		t.Errorf("retained part capacity not reusable: %v", err)
	}
	next, err := f.instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("session not reusable: %v", err)
	}
	if err := f.instance.AbortMultipartUpload(ctx, next.UploadID); err != nil {
		t.Fatal(err)
	}
}

type completionLookupCase struct {
	name string
	err  error
	held bool
}

func TestCompletionLookupReconciliation(t *testing.T) {
	unavailable := errors.New("lookup unavailable")
	cases := []completionLookupCase{
		{"confirmed-missing", storage.ErrUploadNotFound, false},
		{"wrapped-missing", fmt.Errorf("lookup: %w", storage.ErrUploadNotFound), false},
		{"actual-missing", nil, false},
		{"unknown", unavailable, true},
		{"marker-only", errors.Join(storage.ErrMutationCommitted, unavailable), true},
		{"marker-first", ambiguousCompletionLookup("marker-first"), true},
		{"missing-first", ambiguousCompletionLookup("missing-first"), true},
	}
	for _, exists := range []bool{false, true} {
		for _, item := range cases {
			t.Run(fmt.Sprintf("exists=%t/%s", exists, item.name), func(t *testing.T) { checkCompletionLookup(t, exists, item) })
		}
	}
}

func checkCompletionLookup(t *testing.T, exists bool, item completionLookupCase) {
	t.Helper()
	ctx := context.Background()
	native, instance := newAbortFaultInstance(t, exists, 1)
	store := &committedCompletionStore{MemoryStore: native.MemoryStore}
	id := mustCreateUpload(t, ctx, instance, "probe", "target")
	part := mustUploadPart(t, ctx, instance, id, []byte("body"))
	wrapped := &completionLookupFaultStore{MultipartStore: store, lookupErr: item.err}
	instance.multipartStore = wrapped
	meta, err := instance.CompleteMultipartUpload(ctx, id, []storage.PartInfo{*part})
	if meta == nil || meta.Size != 4 || meta.ETag != storage.CompletionETag([]string{part.ETag}) || !errors.Is(err, storage.ErrMutationCommitted) {
		t.Fatalf("committed result=%+v error=%v", meta, err)
	}
	if item.held && !errors.Is(err, item.err) {
		t.Errorf("lookup error lost: %v", err)
	}
	assertCompletionLookupState(t, instance, item.held)
	if wrapped.lookups != 1 {
		t.Errorf("lookups=%d want one", wrapped.lookups)
	}
	wrapped.completed = false
	if item.held {
		if err := instance.AbortMultipartUpload(ctx, id); err != storage.ErrUploadNotFound {
			t.Errorf("confirmed cleanup: %v", err)
		}
	}
	if instance.reservedBytes != 0 || len(instance.multipart) != 0 {
		t.Errorf("confirmed cleanup retained bytes=%d sessions=%d", instance.reservedBytes, len(instance.multipart))
	}
	if _, err := instance.PutObject(ctx, "probe", "target", []byte("12345678"), PutOptions{}); err != nil {
		t.Errorf("capacity stranded: %v", err)
	}
}

func assertCompletionLookupState(t *testing.T, instance *Instance, held bool) {
	t.Helper()
	wantBytes, wantSessions := int64(0), 0
	if held {
		wantBytes, wantSessions = 4, 1
	}
	if instance.reservedBytes != wantBytes || len(instance.multipart) != wantSessions || instance.Usage() != (Usage{Bytes: 4, Objects: 1}) || instance.reservedObjects != 0 {
		t.Errorf("accounting bytes=%d sessions=%d usage=%+v", instance.reservedBytes, len(instance.multipart), instance.Usage())
	}
}
