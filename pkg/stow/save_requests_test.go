package stow_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func filesystemRuntime(t *testing.T, dir string, options stow.Options) *stow.Runtime {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("durable filesystem requests require Linux or macOS")
	}
	r, err := stow.OpenFilesystem(stow.FilesystemOptions{Dir: dir, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestDurableSaveReplayRetainsOriginalEffect(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	r := filesystemRuntime(t, dir, stow.Options{MaxBytes: 20})
	assertFilesystemCapabilities(t, r)
	if err := r.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	_, condition, err := r.ReadForSave(ctx, "guarded", "report")
	if err != nil {
		t.Fatal(err)
	}
	opts := stow.SaveOptions{Condition: condition, RequestKey: "save-one", PutOptions: stow.PutOptions{ContentType: "text/plain", Metadata: map[string]string{"state": "saved"}}}
	original, err := r.SaveObject(ctx, "guarded", "report", []byte("original"), opts)
	if err != nil || original.Outcome != stow.SaveCommitted || original.Replayed {
		t.Fatalf("first=%+v, err=%v", original, err)
	}
	checkRetainedSaveAfterLaterEffects(t, r, opts, original)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = filesystemRuntime(t, dir, stow.Options{})
	resolved, err := r.ResolveSave(ctx, "guarded", "report", "save-one")
	if err != nil || resolved.Outcome != stow.SaveCommitted || !reflect.DeepEqual(resolved.Object, original.Object) {
		t.Fatalf("reopened receipt=%+v, err=%v", resolved, err)
	}
	missing, err := r.ResolveSave(ctx, "guarded", "report", "never-retained")
	if !errors.Is(err, stow.ErrSaveRequestNotFound) || missing.Outcome != stow.SaveUnknown {
		t.Fatalf("missing=%+v err=%v", missing, err)
	}
	if _, err := r.SaveObject(ctx, "guarded", "report", []byte("original"), opts); !errors.Is(err, stow.ErrInvalidSaveCondition) {
		t.Fatalf("old process condition revived: %v", err)
	}
}

func checkRetainedSaveAfterLaterEffects(t *testing.T, r *stow.Runtime, opts stow.SaveOptions, original stow.SaveResult) {
	t.Helper()
	ctx := context.Background()
	if _, err := r.PutObject(ctx, "guarded", "report", []byte("later"), stow.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	usage := r.Usage()
	replay, err := r.SaveObject(ctx, "guarded", "report", []byte("original"), opts)
	if err != nil || replay.Outcome != stow.SaveCommitted || !replay.Replayed || !reflect.DeepEqual(original.Object, replay.Object) {
		t.Fatalf("replay=%+v, original=%+v, err=%v", replay, original, err)
	}
	current, _ := r.GetObject(ctx, "guarded", "report")
	if string(current.Data) != "later" || r.Usage() != usage {
		t.Fatalf("replay changed current=%+v usage=%+v", current, r.Usage())
	}
	if err := r.DeleteObject(ctx, "guarded", "report"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SaveObject(ctx, "guarded", "report", []byte("original"), opts); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetObject(ctx, "guarded", "report"); !errors.Is(err, stow.ErrObjectNotFound) {
		t.Fatalf("replay recreated deleted object: %v", err)
	}
}

func TestDurableSaveRequestCannotChangeMeaning(t *testing.T) {
	ctx := context.Background()
	r := filesystemRuntime(t, t.TempDir(), stow.Options{})
	if err := r.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	options := stow.SaveOptions{Condition: stow.ReplacementCondition(), RequestKey: "immutable"}
	if _, err := r.SaveObject(ctx, "guarded", "report", []byte("fixed"), options); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"bytes", "metadata", "content-type", "condition", "resource"} {
		t.Run(mutation, func(t *testing.T) {
			opts, data, key := options, []byte("fixed"), "report"
			switch mutation {
			case "bytes":
				data = []byte("wrong")
			case "metadata":
				opts.PutOptions.Metadata = map[string]string{"new": "value"}
			case "content-type":
				opts.PutOptions.ContentType = "text/html"
			case "condition":
				_, opts.Condition, _ = r.ReadForSave(ctx, "guarded", "report")
			case "resource":
				key = "elsewhere"
			}
			usage := r.Usage()
			result, err := r.SaveObject(ctx, "guarded", key, data, opts)
			if !errors.Is(err, stow.ErrSaveRequestConflict) || result.Outcome != stow.SaveUnknown || r.Usage() != usage {
				t.Fatalf("meaning change=%+v err=%v usage=%+v", result, err, r.Usage())
			}
		})
	}
}

func TestFilesystemGuardComparesGenerationAndMetadata(t *testing.T) {
	ctx := context.Background()
	r := filesystemRuntime(t, t.TempDir(), stow.Options{})
	if err := r.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	for _, change := range []stow.PutOptions{{}, {Metadata: map[string]string{"changed": "yes"}}, {ContentType: "text/plain"}} {
		if _, err := r.PutObject(ctx, "guarded", "report", []byte("same"), stow.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		_, condition, err := r.ReadForSave(ctx, "guarded", "report")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.PutObject(ctx, "guarded", "report", []byte("same"), change); err != nil {
			t.Fatal(err)
		}
		result, err := r.SaveObject(ctx, "guarded", "report", []byte("stale"), stow.SaveOptions{Condition: condition})
		if !errors.Is(err, stow.ErrSaveConflict) || result.Outcome != stow.SaveNotCommitted {
			t.Fatalf("stale=%+v err=%v", result, err)
		}
	}
}

func TestDurableSaveReplayPrecedesChangedQuotaAdmission(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	r := filesystemRuntime(t, dir, stow.Options{MaxBytes: 4})
	if err := r.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	opts := stow.SaveOptions{Condition: stow.ReplacementCondition(), RequestKey: "quota-replay"}
	saved, err := r.SaveObject(ctx, "guarded", "report", []byte("four"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteObject(ctx, "guarded", "report"); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = filesystemRuntime(t, dir, stow.Options{MaxBytes: 1})
	replay, err := r.SaveObject(ctx, "guarded", "report", []byte("four"), opts)
	if err != nil || !replay.Replayed || !reflect.DeepEqual(replay.Object, saved.Object) || r.Usage() != (stow.Usage{}) {
		t.Fatalf("replay=%+v err=%v usage=%+v", replay, err, r.Usage())
	}
	result, err := r.SaveObject(ctx, "guarded", "report", []byte("four"), stow.SaveOptions{Condition: stow.ReplacementCondition(), RequestKey: "new-request"})
	if !errors.Is(err, stow.ErrQuotaExceeded) || result.Outcome != stow.SaveNotCommitted {
		t.Fatalf("new request=%+v err=%v", result, err)
	}
}

func TestDurableSaveCurrentAuthorityAndLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	r := filesystemRuntime(t, dir, stow.Options{})
	if err := r.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	opts := stow.SaveOptions{Condition: stow.ReplacementCondition(), RequestKey: "authority"}
	if _, err := r.SaveObject(ctx, "guarded", "report", []byte("private"), opts); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	readOnly := stow.ReadOnly()
	r = filesystemRuntime(t, dir, stow.Options{Authority: &readOnly})
	resolved, resolveErr := r.ResolveSave(ctx, "guarded", "report", "authority")
	assertCommittedSave(t, resolved, resolveErr)
	denied, err := r.SaveObject(ctx, "guarded", "report", []byte("private"), opts)
	var unauthorized *stow.ErrNotAuthorized
	if !errors.As(err, &unauthorized) || denied.Outcome != stow.SaveUnknown {
		t.Fatalf("read-only replay=%+v %v", denied, err)
	}
	_ = r.Close()
	writeOnly := stow.AllowNone().With(stow.ObjectWrite)
	r = filesystemRuntime(t, dir, stow.Options{Authority: &writeOnly})
	denied, err = r.SaveObject(ctx, "guarded", "report", []byte("private"), opts)
	if !errors.As(err, &unauthorized) || denied.Outcome != stow.SaveUnknown {
		t.Fatalf("write-only replay=%+v %v", denied, err)
	}
	denied, err = r.ResolveSave(ctx, "guarded", "report", "authority")
	if !errors.As(err, &unauthorized) || denied.Outcome != stow.SaveUnknown {
		t.Fatalf("write-only resolution=%+v %v", denied, err)
	}
	_ = r.Close()
	r = filesystemRuntime(t, dir, stow.Options{})
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, call := range []func(context.Context) (stow.SaveResult, error){
		func(ctx context.Context) (stow.SaveResult, error) {
			return r.ResolveSave(ctx, "guarded", "report", "authority")
		},
		func(ctx context.Context) (stow.SaveResult, error) {
			return r.SaveObject(ctx, "guarded", "report", []byte("private"), opts)
		},
	} {
		result, err := call(canceled)
		if !errors.Is(err, context.Canceled) || result.Outcome != stow.SaveUnknown {
			t.Fatalf("canceled=%+v %v", result, err)
		}
	}
	_ = r.Close()
	result, err := r.ResolveSave(ctx, "guarded", "report", "authority")
	if !errors.Is(err, stow.ErrClosed) || result.Outcome != stow.SaveUnknown {
		t.Fatalf("closed=%+v %v", result, err)
	}
}

func TestSaveRequestUnqualifiedProfilesRefuseBeforeMutation(t *testing.T) {
	ctx := context.Background()
	r := guardedRuntime(t, stow.Options{})
	opts := stow.SaveOptions{Condition: stow.ReplacementCondition(), RequestKey: "requires-durability"}
	result, err := r.SaveObject(ctx, "guarded", "new", []byte("unsafe"), opts)
	if !errors.Is(err, stow.ErrSaveRequestsUnsupported) || result.Outcome != stow.SaveUnknown || r.Usage() != (stow.Usage{}) || r.Capabilities().DurableSaveRequests {
		t.Fatalf("volatile=%+v %v", result, err)
	}
	for _, backend := range []string{"workspace", "supplied-runtime"} {
		t.Run(backend, func(t *testing.T) {
			r, bucket := conditionalRuntime(t, backend)
			result, err := r.SaveObject(ctx, bucket, "new", []byte("unsafe"), opts)
			if !errors.Is(err, stow.ErrSaveRequestsUnsupported) || result.Outcome != stow.SaveUnknown || r.Usage() != (stow.Usage{}) {
				t.Fatalf("unqualified=%+v %v", result, err)
			}
		})
	}
}

func TestFilesystemConstructorValidationOwnershipAndLockCleanup(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("durable filesystem requests require Linux or macOS")
	}
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := stow.OpenFilesystem(stow.FilesystemOptions{Dir: dir, Options: stow.Options{MaxBytes: -1}}); err == nil {
		t.Fatal("negative quota accepted")
	}
	r := filesystemRuntime(t, dir, stow.Options{})
	if _, err := stow.OpenFilesystem(stow.FilesystemOptions{Dir: dir}); err == nil {
		t.Fatal("same directory acquired twice")
	}
	if err := r.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PutObject(ctx, "guarded", "large", []byte("larger"), stow.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Reset(ctx); err == nil {
		t.Fatal("directory reset allowed")
	}
	_ = r.Close()
	if _, err := stow.OpenFilesystem(stow.FilesystemOptions{Dir: dir, Options: stow.Options{MaxBytes: 1}}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Fatalf("initialization quota=%v", err)
	}
	r = filesystemRuntime(t, dir, stow.Options{})
	object, err := r.GetObject(ctx, "guarded", "large")
	if err != nil || string(object.Data) != "larger" {
		t.Fatalf("failed constructor changed data=%+v %v", object, err)
	}
	for _, opts := range []stow.FilesystemOptions{{}, {Dir: t.TempDir(), Options: stow.Options{Backend: stow.BackendMemory}}, {Dir: t.TempDir(), Options: stow.Options{Store: r}}} {
		if _, err := stow.OpenFilesystem(opts); err == nil {
			t.Fatalf("invalid options accepted: %+v", opts)
		}
	}
	if _, err := stow.Open(stow.Options{Backend: stow.BackendFilesystem}); !errors.Is(err, stow.ErrUnsupportedBackend) {
		t.Fatalf("Open silently changed profile: %v", err)
	}
}

func assertCommittedSave(t *testing.T, result stow.SaveResult, err error) {
	t.Helper()
	if err != nil || result.Outcome != stow.SaveCommitted {
		t.Fatalf("save=%+v err=%v", result, err)
	}
}

func assertFilesystemCapabilities(t *testing.T, r *stow.Runtime) {
	t.Helper()
	if caps := r.Capabilities(); !caps.Persistent || !caps.GuardedSaves || !caps.DurableSaveRequests || caps.Backend != stow.BackendFilesystem {
		t.Fatalf("capabilities=%+v", caps)
	}
}

func TestDurableSaveConcurrentWritersPublishOneEffect(t *testing.T) {
	ctx := context.Background()
	r := filesystemRuntime(t, t.TempDir(), stow.Options{})
	if err := r.CreateBucket(ctx, "guarded"); err != nil {
		t.Fatal(err)
	}
	_, condition, err := r.ReadForSave(ctx, "guarded", "race")
	if err != nil {
		t.Fatal(err)
	}
	type attempt struct {
		requestKey string
		result     stow.SaveResult
		err        error
	}
	results := make(chan attempt, 2)
	start := make(chan struct{})
	for _, requestKey := range []string{"first-writer", "second-writer"} {
		go func() {
			<-start
			result, err := r.SaveObject(ctx, "guarded", "race", []byte(requestKey), stow.SaveOptions{Condition: condition, RequestKey: requestKey})
			results <- attempt{requestKey: requestKey, result: result, err: err}
		}()
	}
	close(start)
	winner := ""
	for range 2 {
		attempt := <-results
		if attempt.err == nil {
			if winner != "" || attempt.result.Outcome != stow.SaveCommitted {
				t.Fatalf("multiple committed writers: %+v", attempt)
			}
			winner = attempt.requestKey
			continue
		}
		if !errors.Is(attempt.err, stow.ErrSaveConflict) || attempt.result.Outcome != stow.SaveNotCommitted {
			t.Fatalf("losing writer=%+v", attempt)
		}
	}
	if winner == "" {
		t.Fatal("no writer committed")
	}
	checkConcurrentSaveWinner(t, r, winner)
}

func checkConcurrentSaveWinner(t *testing.T, r *stow.Runtime, winner string) {
	t.Helper()
	ctx := context.Background()
	current, err := r.GetObject(ctx, "guarded", "race")
	if err != nil || string(current.Data) != winner {
		t.Fatalf("published winner=%+v err=%v", current, err)
	}
	receipt, err := r.ResolveSave(ctx, "guarded", "race", winner)
	assertCommittedSave(t, receipt, err)
	if receipt.Object.ETag != current.ETag || receipt.Object.Size != current.Size || r.Usage() != (stow.Usage{Bytes: int64(len(winner)), Objects: 1}) {
		t.Fatalf("receipt=%+v current=%+v usage=%+v", receipt, current, r.Usage())
	}
}
