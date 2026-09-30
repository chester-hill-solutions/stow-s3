package stow_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func guardedRuntime(t *testing.T, options stow.Options) *stow.Runtime {
	t.Helper()
	runtime, err := stow.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	if err := runtime.CreateBucket(context.Background(), "guarded"); err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestGuardedSaveDetectsChangedGenerationAndProperties(t *testing.T) {
	for _, change := range []string{"same-bytes", "metadata", "content-type", "ABA", "delete-recreate", "reset"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			runtime := guardedRuntime(t, stow.Options{})
			put := func(data string, options stow.PutOptions) {
				t.Helper()
				if _, err := runtime.PutObject(ctx, "guarded", "report", []byte(data), options); err != nil {
					t.Fatal(err)
				}
			}
			put("A", stow.PutOptions{})
			_, condition, err := runtime.ReadForSave(ctx, "guarded", "report")
			if err != nil || condition.ObservedAbsence() {
				t.Fatalf("read: absent=%v, err=%v", condition.ObservedAbsence(), err)
			}
			advanceGuardedGeneration(t, runtime, change)
			before, _ := runtime.GetObject(ctx, "guarded", "report")
			usage := runtime.Usage()
			result, err := runtime.SaveObject(ctx, "guarded", "report", []byte("stale"), stow.SaveOptions{Condition: condition, PutOptions: stow.PutOptions{}})
			if !errors.Is(err, stow.ErrSaveConflict) || result.Outcome != stow.SaveNotCommitted {
				t.Fatalf("stale save: %+v, %v", result, err)
			}
			after, _ := runtime.GetObject(ctx, "guarded", "report")
			if !reflect.DeepEqual(before, after) || runtime.Usage() != usage {
				t.Fatal("refused save changed bytes, properties or usage")
			}
		})
	}
}

func advanceGuardedGeneration(t *testing.T, runtime *stow.Runtime, change string) {
	t.Helper()
	ctx := context.Background()
	put := func(data string, options stow.PutOptions) {
		t.Helper()
		if _, err := runtime.PutObject(ctx, "guarded", "report", []byte(data), options); err != nil {
			t.Fatal(err)
		}
	}
	switch change {
	case "metadata":
		put("A", stow.PutOptions{Metadata: map[string]string{"reviewed": "yes"}})
	case "content-type":
		put("A", stow.PutOptions{ContentType: "text/plain"})
	case "ABA":
		put("B", stow.PutOptions{})
		put("A", stow.PutOptions{})
	case "delete-recreate":
		if err := runtime.DeleteObject(ctx, "guarded", "report"); err != nil {
			t.Fatal(err)
		}
		put("A", stow.PutOptions{})
	case "reset":
		if err := runtime.Reset(ctx); err != nil {
			t.Fatal(err)
		}
		if err := runtime.CreateBucket(ctx, "guarded"); err != nil {
			t.Fatal(err)
		}
		put("A", stow.PutOptions{})
	default:
		put("A", stow.PutOptions{})
	}
}

func TestGuardedSaveAbsenceAndExplicitReplacement(t *testing.T) {
	ctx := context.Background()
	runtime := guardedRuntime(t, stow.Options{})
	if !runtime.Capabilities().GuardedSaves {
		t.Fatal("memory guarded-save capability missing")
	}
	_, condition, err := runtime.ReadForSave(ctx, "guarded", "new")
	if err != nil || !condition.ObservedAbsence() {
		t.Fatalf("absence observation: %v, %v", condition, err)
	}
	first, err := runtime.SaveObject(ctx, "guarded", "new", []byte("created"), stow.SaveOptions{Condition: condition, PutOptions: stow.PutOptions{}})
	if err != nil || first.Outcome != stow.SaveCommitted {
		t.Fatalf("create: %+v, %v", first, err)
	}
	if result, err := runtime.SaveObject(ctx, "guarded", "new", []byte("collision"), stow.SaveOptions{Condition: condition, PutOptions: stow.PutOptions{}}); !errors.Is(err, stow.ErrSaveConflict) || result.Outcome != stow.SaveNotCommitted {
		t.Fatalf("create collision: %+v, %v", result, err)
	}
	if _, _, err := runtime.ReadForSave(ctx, "missing", "new"); !errors.Is(err, stow.ErrBucketNotFound) {
		t.Fatalf("missing bucket treated as absence: %v", err)
	}
	result, err := runtime.SaveObject(ctx, "guarded", "new", []byte("replacement"), stow.SaveOptions{Condition: stow.ReplacementCondition(), PutOptions: stow.PutOptions{}})
	if err != nil || result.Outcome != stow.SaveCommitted {
		t.Fatalf("explicit replacement: %+v, %v", result, err)
	}
	_, current, _ := runtime.ReadForSave(ctx, "guarded", "new")
	result, err = runtime.SaveObject(ctx, "guarded", "new", []byte("edited"), stow.SaveOptions{Condition: current, PutOptions: stow.PutOptions{Metadata: map[string]string{"state": "saved"}}})
	if err != nil || result.Outcome != stow.SaveCommitted || result.Object.Metadata["state"] != "saved" {
		t.Fatalf("matching edit: %+v, %v", result, err)
	}
}

func TestGuardedSaveRejectsUnboundOrAmbiguousConditions(t *testing.T) {
	ctx := context.Background()
	first, second := guardedRuntime(t, stow.Options{}), guardedRuntime(t, stow.Options{})
	_, condition, _ := first.ReadForSave(ctx, "guarded", "new")
	for _, test := range []struct {
		name      string
		runtime   *stow.Runtime
		key       string
		condition stow.SaveCondition
		options   stow.PutOptions
	}{
		{"zero", first, "new", stow.SaveCondition{}, stow.PutOptions{}},
		{"other-key", first, "other", condition, stow.PutOptions{}},
		{"other-runtime", second, "new", condition, stow.PutOptions{}},
		{"extra-condition", first, "new", condition, stow.PutOptions{IfNoneMatch: "*"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.runtime.SaveObject(ctx, "guarded", test.key, []byte("wrong"), stow.SaveOptions{Condition: test.condition, PutOptions: test.options})
			if !errors.Is(err, stow.ErrInvalidSaveCondition) || result.Outcome != stow.SaveNotCommitted || test.runtime.Usage() != (stow.Usage{}) {
				t.Fatalf("invalid condition: %+v, %v, usage=%+v", result, err, test.runtime.Usage())
			}
		})
	}
}

func TestGuardedSaveTwoWritersOneCommit(t *testing.T) {
	ctx := context.Background()
	runtime := guardedRuntime(t, stow.Options{})
	_, condition, _ := runtime.ReadForSave(ctx, "guarded", "race")
	var group sync.WaitGroup
	results := make(chan stow.SaveOutcome, 2)
	for range 2 {
		group.Go(func() {
			result, err := runtime.SaveObject(ctx, "guarded", "race", []byte("winner"), stow.SaveOptions{Condition: condition, PutOptions: stow.PutOptions{}})
			if err != nil && !errors.Is(err, stow.ErrSaveConflict) {
				t.Errorf("save: %v", err)
			}
			results <- result.Outcome
		})
	}
	group.Wait()
	close(results)
	counts := map[stow.SaveOutcome]int{}
	for result := range results {
		counts[result]++
	}
	if counts[stow.SaveCommitted] != 1 || counts[stow.SaveNotCommitted] != 1 {
		t.Fatalf("outcomes=%v", counts)
	}
	if runtime.Usage() != (stow.Usage{Bytes: 6, Objects: 1}) {
		t.Fatalf("usage=%+v", runtime.Usage())
	}
}

func TestGuardedSaveRefusesUnqualifiedStores(t *testing.T) {
	ctx := context.Background()
	for _, backend := range []string{"workspace", "supplied-runtime"} {
		t.Run(backend, func(t *testing.T) {
			runtime, bucket := conditionalRuntime(t, backend)
			if runtime.Capabilities().GuardedSaves || !runtime.Capabilities().ConditionalWrites {
				t.Fatalf("capabilities=%+v", runtime.Capabilities())
			}
			if _, _, err := runtime.ReadForSave(ctx, bucket, "new"); !errors.Is(err, stow.ErrGuardedSavesUnsupported) {
				t.Fatalf("read: %v", err)
			}
			result, err := runtime.SaveObject(ctx, bucket, "new", []byte("unsafe"), stow.SaveOptions{Condition: stow.ReplacementCondition(), PutOptions: stow.PutOptions{}})
			if !errors.Is(err, stow.ErrGuardedSavesUnsupported) || result.Outcome != stow.SaveNotCommitted || runtime.Usage() != (stow.Usage{}) {
				t.Fatalf("save: %+v, %v, usage=%+v", result, err, runtime.Usage())
			}
		})
	}
}

func TestGuardedSavePreservesQuotaCancellationCloseAndAuthority(t *testing.T) {
	ctx := context.Background()
	runtime := guardedRuntime(t, stow.Options{MaxBytes: 2})
	_, condition, _ := runtime.ReadForSave(ctx, "guarded", "new")
	result, err := runtime.SaveObject(ctx, "guarded", "new", []byte("too big"), stow.SaveOptions{Condition: condition, PutOptions: stow.PutOptions{}})
	if !errors.Is(err, stow.ErrQuotaExceeded) || result.Outcome != stow.SaveNotCommitted || runtime.Usage() != (stow.Usage{}) {
		t.Fatalf("quota: %+v, %v", result, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	result, err = runtime.SaveObject(canceled, "guarded", "new", []byte("ok"), stow.SaveOptions{Condition: condition, PutOptions: stow.PutOptions{}})
	if !errors.Is(err, context.Canceled) || result.Outcome != stow.SaveNotCommitted || runtime.Usage() != (stow.Usage{}) {
		t.Fatalf("cancellation: %+v, %v", result, err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	result, err = runtime.SaveObject(ctx, "guarded", "new", []byte("ok"), stow.SaveOptions{Condition: condition, PutOptions: stow.PutOptions{}})
	if !errors.Is(err, stow.ErrClosed) || result.Outcome != stow.SaveNotCommitted {
		t.Fatalf("closed: %+v, %v", result, err)
	}
	readOnly := stow.ReadOnly()
	denied, err := stow.Open(stow.Options{Authority: &readOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer denied.Close()
	result, err = denied.SaveObject(ctx, "guarded", "new", []byte("ok"), stow.SaveOptions{Condition: stow.ReplacementCondition(), PutOptions: stow.PutOptions{}})
	var unauthorized *stow.ErrNotAuthorized
	if !errors.As(err, &unauthorized) || result.Outcome != stow.SaveNotCommitted || denied.Usage() != (stow.Usage{}) {
		t.Fatalf("replacement authority: %+v, %v", result, err)
	}
}
