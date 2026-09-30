package stow_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func conditionalRuntime(t *testing.T, backend string) (*stow.Runtime, string) {
	t.Helper()
	if backend == "workspace" {
		ws := openWorkspace(t, t.TempDir())
		return ws.Runtime, ws.Bucket()
	}
	options := stow.Options{}
	if backend == "supplied-runtime" {
		_, inner := newPublicRuntime(t)
		options.Store = inner
	}
	runtime, err := stow.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	if err := runtime.CreateBucket(context.Background(), "conditional"); err != nil {
		t.Fatal(err)
	}
	return runtime, "conditional"
}

func assertConditionalState(t *testing.T, runtime *stow.Runtime, want stow.Object, usage stow.Usage, data string) {
	t.Helper()
	want.Data = []byte(data)
	got, err := runtime.GetObject(context.Background(), want.Bucket, want.Key)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("object = %+v, err = %v, want %+v", got, err, want)
	}
	if got := runtime.Usage(); got != usage {
		t.Fatalf("usage = %+v, want %+v", got, usage)
	}
}

func TestConditionalPutRefusesStaleWrites(t *testing.T) {
	for _, backend := range []string{"memory", "workspace", "supplied-runtime"} {
		t.Run(backend, func(t *testing.T) {
			runtime, bucket := conditionalRuntime(t, backend)
			ctx := context.Background()
			if !runtime.Capabilities().ConditionalWrites {
				t.Fatal("runtime did not advertise conditional writes")
			}
			original, err := runtime.PutObject(ctx, bucket, "report.txt", []byte("original"), stow.PutOptions{})
			if err != nil {
				t.Fatal(err)
			}
			current, err := runtime.PutObject(ctx, bucket, "report.txt", []byte("current"), stow.PutOptions{
				IfMatch: original.ETag, ContentType: "text/plain", Metadata: map[string]string{"state": "current"},
			})
			if err != nil {
				t.Fatalf("matching write: %v", err)
			}
			usage := runtime.Usage()
			failed, err := runtime.PutObject(ctx, bucket, "report.txt", []byte("stale edit"), stow.PutOptions{
				IfMatch: original.ETag, ContentType: "application/json", Metadata: map[string]string{"state": "stale"},
			})
			if !errors.Is(err, stow.ErrPreconditionFailed) || !reflect.DeepEqual(failed, stow.Object{}) {
				t.Fatalf("stale write = %+v, %v, want zero object and ErrPreconditionFailed", failed, err)
			}
			assertConditionalState(t, runtime, current, usage, "current")
			if _, err := runtime.PutObject(ctx, bucket, "report.txt", []byte("legacy replacement"), stow.PutOptions{}); err != nil {
				t.Fatalf("unconditional legacy write: %v", err)
			}
			got, err := runtime.GetObject(ctx, bucket, "report.txt")
			if err != nil || string(got.Data) != "legacy replacement" {
				t.Fatalf("legacy replacement = %q, %v", got.Data, err)
			}
		})
	}
}

func TestConditionalPutRequiresExpectedAbsence(t *testing.T) {
	for _, backend := range []string{"memory", "workspace", "supplied-runtime"} {
		t.Run(backend, func(t *testing.T) {
			runtime, bucket := conditionalRuntime(t, backend)
			ctx := context.Background()
			if _, err := runtime.PutObject(ctx, bucket, "report.txt", []byte("missing edit"), stow.PutOptions{IfMatch: "*"}); !errors.Is(err, stow.ErrPreconditionFailed) {
				t.Fatalf("match on missing object = %v, want ErrPreconditionFailed", err)
			}
			if _, err := runtime.GetObject(ctx, bucket, "report.txt"); !errors.Is(err, stow.ErrObjectNotFound) {
				t.Fatalf("refused write created an object: %v", err)
			}
			if got := runtime.Usage(); got != (stow.Usage{}) {
				t.Fatalf("refused missing write usage = %+v", got)
			}
			created, err := runtime.PutObject(ctx, bucket, "report.txt", []byte("created"), stow.PutOptions{
				IfNoneMatch: "*", Metadata: map[string]string{"state": "created"},
			})
			if err != nil {
				t.Fatalf("create if absent: %v", err)
			}
			usage := runtime.Usage()
			for _, condition := range []string{"*", created.ETag} {
				if _, err := runtime.PutObject(ctx, bucket, "report.txt", []byte("clobbered"), stow.PutOptions{IfNoneMatch: condition}); !errors.Is(err, stow.ErrPreconditionFailed) {
					t.Fatalf("IfNoneMatch %q = %v, want ErrPreconditionFailed", condition, err)
				}
				assertConditionalState(t, runtime, created, usage, "created")
			}
		})
	}
}

func TestConditionalPutAllowsOneConcurrentWriter(t *testing.T) {
	for _, backend := range []string{"memory", "workspace", "supplied-runtime"} {
		t.Run(backend, func(t *testing.T) {
			runtime, bucket := conditionalRuntime(t, backend)
			ctx := context.Background()
			original, err := runtime.PutObject(ctx, bucket, "report.txt", []byte("original"), stow.PutOptions{})
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var writers sync.WaitGroup
			for index := range 2 {
				writers.Add(1)
				go func() {
					defer writers.Done()
					<-start
					_, err := runtime.PutObject(ctx, bucket, "report.txt", []byte(fmt.Sprintf("writer-%d", index)), stow.PutOptions{IfMatch: original.ETag})
					results <- err
				}()
			}
			close(start)
			writers.Wait()
			close(results)
			accepted, refused := 0, 0
			for err := range results {
				switch {
				case err == nil:
					accepted++
				case errors.Is(err, stow.ErrPreconditionFailed):
					refused++
				default:
					t.Fatalf("concurrent write: %v", err)
				}
			}
			if accepted != 1 || refused != 1 {
				t.Fatalf("accepted %d, refused %d, want one of each", accepted, refused)
			}
			got, err := runtime.GetObject(ctx, bucket, "report.txt")
			if err != nil || (string(got.Data) != "writer-0" && string(got.Data) != "writer-1") {
				t.Fatalf("winner = %q, %v", got.Data, err)
			}
			if got := runtime.Usage(); got != (stow.Usage{Bytes: 8, Objects: 1}) {
				t.Fatalf("winner usage = %+v", got)
			}
		})
	}
}

func TestConditionalPutRefusesUnqualifiedSuppliedStore(t *testing.T) {
	store := newMemoryStore()
	runtime, err := stow.Open(stow.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	ctx := context.Background()
	if runtime.Capabilities().ConditionalWrites {
		t.Fatal("legacy store advertised conditional writes")
	}
	if err := runtime.CreateBucket(ctx, "conditional"); err != nil {
		t.Fatal(err)
	}
	for _, options := range []stow.PutOptions{{IfMatch: "*"}, {IfNoneMatch: "*"}} {
		if _, err := runtime.PutObject(ctx, "conditional", "report.txt", []byte("unsafe"), options); !errors.Is(err, stow.ErrConditionalWritesUnsupported) {
			t.Fatalf("unqualified conditional write = %v, want ErrConditionalWritesUnsupported", err)
		}
	}
	if _, err := runtime.GetObject(ctx, "conditional", "report.txt"); !errors.Is(err, stow.ErrObjectNotFound) {
		t.Fatalf("unsupported write created an object: %v", err)
	}
	if got := runtime.Usage(); got != (stow.Usage{}) {
		t.Fatalf("unsupported write usage = %+v", got)
	}
	if _, err := runtime.PutObject(ctx, "conditional", "report.txt", []byte("legacy"), stow.PutOptions{}); err != nil {
		t.Fatalf("legacy supplied-store write: %v", err)
	}
}
