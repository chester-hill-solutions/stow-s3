package embedded

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type batchDeleteStore struct {
	Store
	missing error
	failure error
	calls   []string
}

func (s *batchDeleteStore) DeleteObject(ctx context.Context, bucket, key string) error {
	s.calls = append(s.calls, key)
	if key == "stop" && s.failure != nil {
		return s.failure
	}
	if _, err := s.Store.HeadObject(ctx, bucket, key); err != nil {
		if errors.Is(err, ErrObjectNotFound) {
			return s.missing
		}
		return err
	}
	return s.Store.DeleteObject(ctx, bucket, key)
}

func openBatchDeleteFixture(t *testing.T, missing error) (*Runtime, *batchDeleteStore) {
	t.Helper()
	base, err := Open(Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	ctx := context.Background()
	if err := base.CreateBucket(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"first", "last"} {
		if _, err := base.PutObject(ctx, "probe", key, []byte("body"), PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	store := &batchDeleteStore{Store: base, missing: missing}
	outer, err := Open(Options{Store: store, MaxBytes: 8, MaxObjects: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = outer.Close() })
	return outer, store
}

func TestCustomStoreBatchDeleteMissingAndDuplicates(t *testing.T) {
	missingErrors := []error{ErrObjectNotFound, fmt.Errorf("missing: %w", ErrObjectNotFound), storage.ErrObjectNotFound, fmt.Errorf("missing: %w", storage.ErrObjectNotFound)}
	for n, missing := range missingErrors {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			outer, store := openBatchDeleteFixture(t, missing)
			keys := []string{"missing", "first", "first", "last", "missing"}
			deleted, err := UnwrapInstance(outer).DeleteObjects(context.Background(), "probe", keys)
			if err != nil || !reflect.DeepEqual(deleted, keys) || !reflect.DeepEqual(store.calls, keys) {
				t.Fatalf("deleted=%v calls=%v error=%v, want %v", deleted, store.calls, err, keys)
			}
			assertBatchDeleteState(t, outer, store.Store, 0)
		})
	}
}

func TestCustomStoreBatchDeletePreservesFailurePrefix(t *testing.T) {
	cause := errors.New("delete unavailable")
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{"storage", fmt.Errorf("delete: %w", cause), cause},
		{"precondition", fmt.Errorf("delete: %w", ErrPreconditionFailed), storage.ErrPreconditionFailed},
		{"bucket", ErrBucketNotFound, storage.ErrBucketNotFound},
		{"cancellation", context.Canceled, context.Canceled},
		{"committed", errors.Join(ErrMutationCommitted, ErrObjectNotFound), ErrMutationCommitted},
	} {
		t.Run(test.name, func(t *testing.T) {
			outer, store := openBatchDeleteFixture(t, ErrObjectNotFound)
			store.failure = test.err
			keys := []string{"missing", "first", "stop", "last"}
			deleted, err := UnwrapInstance(outer).DeleteObjects(context.Background(), "probe", keys)
			if !errors.Is(err, test.want) || !reflect.DeepEqual(deleted, keys[:2]) || !reflect.DeepEqual(store.calls, keys[:3]) {
				t.Fatalf("deleted=%v calls=%v error=%v, want prefix %v error %v", deleted, store.calls, err, keys[:2], test.want)
			}
			assertBatchDeleteState(t, outer, store.Store, 4)
		})
	}
}

func assertBatchDeleteState(t *testing.T, outer *Runtime, base Store, remaining int64) {
	t.Helper()
	want := Usage{Bytes: remaining, Objects: remaining / 4}
	if got := outer.Usage(); got != want {
		t.Fatalf("usage=%+v, want %+v", got, want)
	}
	ctx := context.Background()
	if _, err := base.HeadObject(ctx, "probe", "first"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("first object was not deleted: %v", err)
	}
	last, err := base.GetObject(ctx, "probe", "last")
	if remaining == 0 {
		if !errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("last object was not deleted: %v", err)
		}
	} else if err != nil || string(last.Data) != "body" {
		t.Fatalf("later object changed after failure: %+v %v", last, err)
	}
	if _, err := outer.PutObject(ctx, "probe", "reuse", []byte("body"), PutOptions{}); err != nil {
		t.Fatalf("confirmed deletions did not free quota: %v", err)
	}
}

func TestCustomStoreBatchDeleteS3MissingKey(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		t.Run(fmt.Sprint(quiet), func(t *testing.T) {
			outer, store := openBatchDeleteFixture(t, fmt.Errorf("missing: %w", ErrObjectNotFound))
			adapter, err := stowruntime.NewStoreAdapter(UnwrapInstance(outer))
			if err != nil {
				t.Fatal(err)
			}
			server, err := s3api.New(s3api.Config{Store: adapter, Auth: s3api.DevBypass})
			if err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf("<Delete><Quiet>%t</Quiet><Object><Key>missing</Key></Object><Object><Key>first</Key></Object><Object><Key>first</Key></Object><Object><Key>last</Key></Object></Delete>", quiet)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/probe?delete", strings.NewReader(body))
			request.Header.Set("Content-Length", fmt.Sprint(len(body)))
			request.Header.Set("Content-Type", "application/xml")
			server.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("S3 delete status=%d body=%s", response.Code, response.Body.String())
			}
			var result struct {
				Deleted []struct{ Key string }
				Errors  []struct{ Key string } `xml:"Error"`
			}
			if err := xml.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, entry := range result.Deleted {
				got = append(got, entry.Key)
			}
			var want []string
			if !quiet {
				want = []string{"missing", "first", "first", "last"}
			}
			if len(result.Errors) != 0 || !reflect.DeepEqual(got, want) {
				t.Fatalf("S3 deleted=%v errors=%v, want %v", got, result.Errors, want)
			}
			assertBatchDeleteState(t, outer, store.Store, 0)
		})
	}
}

type batchDeleteBenchmarkStore struct {
	Store
	result error
}

func (s batchDeleteBenchmarkStore) DeleteObject(context.Context, string, string) error {
	return s.result
}

func BenchmarkCustomStoreBatchDelete(b *testing.B) {
	for _, test := range []struct {
		name   string
		result error
	}{
		{"confirmed", nil},
		{"internal-missing", storage.ErrObjectNotFound},
	} {
		b.Run(test.name, func(b *testing.B) {
			adapter := adaptStore(batchDeleteBenchmarkStore{result: test.result})
			keys := make([]string, 1000)
			for n := range keys {
				keys[n] = fmt.Sprint(n)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				deleted, err := adapter.DeleteObjects(context.Background(), "probe", keys)
				if err != nil || len(deleted) != len(keys) {
					b.Fatalf("deleted=%d error=%v", len(deleted), err)
				}
			}
		})
	}
}
