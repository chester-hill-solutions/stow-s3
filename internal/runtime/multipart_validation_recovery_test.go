package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type validationRecoveryStore struct {
	*storage.MemoryStore
	validationErr                       error
	lookupErr                           error
	headErr                             error
	validations, lookups, heads, aborts int
}

func (s *validationRecoveryStore) ValidateMultipartUpload(ctx context.Context, id, bucket, key string) error {
	s.validations++
	if s.validationErr != nil {
		return s.validationErr
	}
	return s.MemoryStore.ValidateMultipartUpload(ctx, id, bucket, key)
}

func (s *validationRecoveryStore) GetMultipartUpload(ctx context.Context, id string) (*storage.MultipartUpload, error) {
	s.lookups++
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.MemoryStore.GetMultipartUpload(ctx, id)
}

func (s *validationRecoveryStore) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	s.heads++
	if s.headErr != nil {
		return nil, s.headErr
	}
	return s.MemoryStore.HeadObject(ctx, bucket, key)
}

func (s *validationRecoveryStore) AbortMultipartUpload(ctx context.Context, id string) error {
	s.aborts++
	return s.MemoryStore.AbortMultipartUpload(ctx, id)
}

type validationRecoveryFixture struct {
	instance *Instance
	native   *abortFaultStore
	store    *validationRecoveryStore
	id       string
	exists   bool
}

func newValidationRecoveryFixture(t *testing.T, exists, retained bool) *validationRecoveryFixture {
	t.Helper()
	native, instance := newAbortFaultInstance(t, exists, 1)
	id := nativeAbortUpload(t, instance, "part")
	native.failure, native.refuse = errors.New("abort acknowledgement unavailable"), retained
	if err := instance.AbortMultipartUpload(context.Background(), id); err != native.failure {
		t.Fatal(err)
	}
	assertNativeAbortState(t, instance, exists, true)
	store := &validationRecoveryStore{MemoryStore: native.MemoryStore}
	instance.store, instance.multipartStore = store, store
	return &validationRecoveryFixture{instance: instance, native: native, store: store, id: id, exists: exists}
}

type validationRecoveryCase struct {
	name                              string
	validationErr, lookupErr, headErr error
	bucket, key                       string
	untracked, retained, released     bool
	lookups, heads                    int
}

func TestMultipartValidationRecovery(t *testing.T) {
	unavailable := errors.New("observation unavailable")
	cases := []validationRecoveryCase{
		{name: "missing", released: true, lookups: 1, heads: 1},
		{name: "upload-missing", validationErr: storage.ErrUploadNotFound, released: true, lookups: 1, heads: 1},
		{name: "wrapped-missing", validationErr: fmt.Errorf("validate: %w", storage.ErrNoSuchUpload), lookupErr: fmt.Errorf("lookup: %w", storage.ErrUploadNotFound), released: true, lookups: 1, heads: 1},
		{name: "wrong-bucket", bucket: "other"},
		{name: "wrong-key", key: "other"},
		{name: "retained", retained: true, validationErr: storage.ErrNoSuchUpload, lookups: 1},
		{name: "lookup-unavailable", lookupErr: unavailable, lookups: 1},
		{name: "lookup-committed-missing", lookupErr: errors.Join(storage.ErrMutationCommitted, storage.ErrUploadNotFound), lookups: 1},
		{name: "validation-unavailable", validationErr: unavailable},
		{name: "validation-committed-missing", validationErr: errors.Join(storage.ErrMutationCommitted, storage.ErrNoSuchUpload)},
		{name: "head-unavailable", headErr: unavailable, lookups: 1, heads: 1},
		{name: "head-committed-missing", headErr: errors.Join(storage.ErrMutationCommitted, storage.ErrObjectNotFound), lookups: 1, heads: 1},
		{name: "untracked", untracked: true},
		{name: "success", retained: true},
	}
	for _, exists := range []bool{false, true} {
		for _, item := range cases {
			t.Run(fmt.Sprintf("exists=%t/%s", exists, item.name), func(t *testing.T) { checkValidationRecovery(t, exists, item) })
		}
	}
}

func checkValidationRecovery(t *testing.T, exists bool, item validationRecoveryCase) {
	t.Helper()
	f := newValidationRecoveryFixture(t, exists, item.retained)
	f.store.validationErr, f.store.lookupErr, f.store.headErr = item.validationErr, item.lookupErr, item.headErr
	id, bucket, key := validationRecoveryRequest(f, item)
	want := storage.ErrNoSuchUpload
	if item.validationErr != nil {
		want = item.validationErr
	}
	if item.name == "success" {
		want = nil
	}
	if err := f.instance.ValidateMultipartUpload(context.Background(), id, bucket, key); err != want {
		t.Errorf("validation error=%v want identical %v", err, want)
	}
	if f.store.validations != 1 || f.store.lookups != item.lookups || f.store.heads != item.heads {
		t.Errorf("provider calls validation=%d lookup=%d head=%d want 1/%d/%d", f.store.validations, f.store.lookups, f.store.heads, item.lookups, item.heads)
	}
	assertNativeAbortState(t, f.instance, exists, !item.released)
	finishValidationRecovery(t, f, item.retained)
}

func validationRecoveryRequest(f *validationRecoveryFixture, item validationRecoveryCase) (string, string, string) {
	id, bucket, key := f.id, "probe", "target"
	if item.bucket != "" {
		bucket = item.bucket
	}
	if item.key != "" {
		key = item.key
	}
	if item.untracked {
		id = "unknown"
	}
	return id, bucket, key
}

func finishValidationRecovery(t *testing.T, f *validationRecoveryFixture, retained bool) {
	t.Helper()
	ctx := context.Background()
	f.store.validationErr, f.store.lookupErr, f.store.headErr = nil, nil, nil
	if retained {
		parts, err := f.store.ListParts(ctx, f.id)
		if err != nil || len(parts) != 1 || parts[0].Size != 4 {
			t.Fatalf("lost retained part: %+v %v", parts, err)
		}
		if err := f.instance.AbortMultipartUpload(ctx, f.id); err != nil {
			t.Fatal(err)
		}
	} else if err := f.instance.ValidateMultipartUpload(ctx, f.id, "probe", "target"); err != storage.ErrNoSuchUpload {
		t.Fatal(err)
	}
	assertNativeAbortState(t, f.instance, f.exists, false)
	f.store.lookups, f.store.heads = 0, 0
	if err := f.instance.ValidateMultipartUpload(ctx, f.id, "probe", "target"); err != storage.ErrNoSuchUpload {
		t.Fatal(err)
	}
	if f.store.lookups != 0 || f.store.heads != 0 {
		t.Errorf("repeat validation performed recovery reads: lookup=%d head=%d", f.store.lookups, f.store.heads)
	}
	checkNativeAbortCapacity(t, f.instance, f.native)
}

func TestMultipartValidationRecoveryAuthorization(t *testing.T) {
	for _, gate := range []string{"authority", "policy", "cancelled"} {
		t.Run(gate, func(t *testing.T) {
			f := newValidationRecoveryFixture(t, false, false)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch gate {
			case "authority":
				f.instance.authority = authority.None()
			case "policy":
				f.instance.policy = policy.Fixed(&policy.Set{})
			case "cancelled":
				cancel()
			}
			err := f.instance.ValidateMultipartUpload(ctx, f.id, "probe", "target")
			assertValidationGateError(t, gate, err)
			if f.store.validations != 0 || f.store.lookups != 0 || f.store.heads != 0 {
				t.Fatalf("denied validation reached provider: %+v", f.store)
			}
			f.instance.authority, f.instance.policy = authority.All(), nil
			assertNativeAbortState(t, f.instance, false, true)
		})
	}
}

func TestMultipartValidationRecoveryKeepsSharedTarget(t *testing.T) {
	ctx := context.Background()
	native, instance := newAbortFaultInstance(t, false, 2)
	first := nativeAbortUpload(t, instance, "part")
	second := nativeAbortUpload(t, instance, "xx")
	native.failure = errors.New("abort acknowledgement unavailable")
	if err := instance.AbortMultipartUpload(ctx, first); err != native.failure {
		t.Fatal(err)
	}
	if err := instance.ValidateMultipartUpload(ctx, first, "probe", "target"); err != storage.ErrNoSuchUpload {
		t.Fatal(err)
	}
	if instance.reservedBytes != 2 || instance.reservedObjects != 1 || len(instance.multipart) != 1 {
		t.Errorf("surviving quota bytes=%d objects=%d sessions=%d", instance.reservedBytes, instance.reservedObjects, len(instance.multipart))
	}
	if _, err := instance.PutObject(ctx, "probe", "target", []byte("1234567"), PutOptions{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("surviving part escaped byte quota: %v", err)
	}
	third, err := instance.CreateMultipartUpload(ctx, "probe", "target", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("absent upload stranded session: %v", err)
	}
	native.failure = nil
	for _, id := range []string{second, third.UploadID} {
		if err := instance.AbortMultipartUpload(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if instance.reservedBytes != 0 || instance.reservedObjects != 0 || len(instance.multipart) != 0 {
		t.Fatalf("final reservation leak: %+v", instance.usageState)
	}
}

func TestConcurrentMultipartValidationRecovery(t *testing.T) {
	f := newValidationRecoveryFixture(t, false, false)
	start := make(chan struct{})
	done := make(chan error, 2)
	for n := 0; n < 2; n++ {
		go func() {
			<-start
			done <- f.instance.ValidateMultipartUpload(context.Background(), f.id, "probe", "target")
		}()
	}
	close(start)
	for n := 0; n < 2; n++ {
		if err := <-done; err != storage.ErrNoSuchUpload {
			t.Errorf("retry error=%v", err)
		}
	}
	if f.store.lookups != 1 || f.store.heads != 1 {
		t.Errorf("duplicate/missing recovery reads: lookup=%d head=%d", f.store.lookups, f.store.heads)
	}
	assertNativeAbortState(t, f.instance, false, false)
	checkNativeAbortCapacity(t, f.instance, f.native)
}

func assertValidationGateError(t *testing.T, gate string, err error) {
	t.Helper()
	switch gate {
	case "authority":
		var denied *authority.ErrNotAuthorized
		if !errors.As(err, &denied) {
			t.Fatalf("authority error=%v", err)
		}
	case "policy":
		if !policy.IsRefusal(err) {
			t.Fatalf("policy error=%v", err)
		}
	case "cancelled":
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled error=%v", err)
		}
	}
}

func TestReadOnlyMultipartValidationCanReconcile(t *testing.T) {
	f := newValidationRecoveryFixture(t, false, false)
	f.instance.authority = authority.ReadOnly()
	if err := f.instance.ValidateMultipartUpload(context.Background(), f.id, "probe", "target"); err != storage.ErrNoSuchUpload {
		t.Fatal(err)
	}
	if f.store.lookups != 1 || f.store.heads != 1 || f.store.aborts != 0 {
		t.Errorf("observation calls: lookup=%d head=%d abort=%d", f.store.lookups, f.store.heads, f.store.aborts)
	}
	assertNativeAbortState(t, f.instance, false, false)
	f.instance.authority = authority.All()
	checkNativeAbortCapacity(t, f.instance, f.native)
}

func TestMultipartValidationPolicyDoesNotReadAnotherTarget(t *testing.T) {
	f := newValidationRecoveryFixture(t, false, false)
	var set policy.Set
	set.Add("runtime", "other", policy.Selector{Kind: policy.KindObject, Prefix: "target"}, policy.Allow, authority.ObjectRead)
	f.instance.policy = policy.Fixed(&set)
	if err := f.instance.ValidateMultipartUpload(context.Background(), f.id, "other", "target"); err != storage.ErrNoSuchUpload {
		t.Fatal(err)
	}
	if f.store.validations != 1 || f.store.lookups != 0 || f.store.heads != 0 || f.store.aborts != 0 {
		t.Fatalf("wrong-route observation reached cached target: %+v", f.store)
	}
	if err := f.instance.ValidateMultipartUpload(context.Background(), f.id, "probe", "target"); !policy.IsRefusal(err) {
		t.Fatalf("uncovered target error=%v", err)
	}
	if f.store.validations != 1 {
		t.Fatalf("denied request reached provider: %d", f.store.validations)
	}
	f.instance.policy = nil
	assertNativeAbortState(t, f.instance, false, true)
}
