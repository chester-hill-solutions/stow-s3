package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type unguardedReadStore struct{ storage.Store }

func TestReadForSaveRefusalDoesNotRead(t *testing.T) {
	for _, reason := range []string{"cancelled", "authority", "policy", "closed", "unsupported"} {
		t.Run(reason, func(t *testing.T) { checkSaveReadAccess(t, reason) })
	}
}

func checkSaveReadAccess(t *testing.T, reason string) {
	t.Helper()
	f := newSaveReadFixture(t, true)
	ctx := prepareSaveReadRefusal(t, f, reason)
	_, condition, err := f.instance.ReadForSave(ctx, "probe", "target")
	if err == nil || condition != (SaveCondition{}) || f.store.gets != 0 {
		t.Fatalf("refusal=%v condition=%+v reads=%d", err, condition, f.store.gets)
	}
	expected := map[string]error{"cancelled": context.Canceled, "closed": ErrClosed, "unsupported": ErrGuardedSavesUnsupported}[reason]
	if expected != nil && !errors.Is(err, expected) {
		t.Fatalf("refusal=%v want %v", err, expected)
	}
}

func prepareSaveReadRefusal(t *testing.T, f *saveReadFixture, reason string) context.Context {
	t.Helper()
	ctx := context.Background()
	switch reason {
	case "cancelled":
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		ctx = cancelled
	case "authority":
		f.instance.authority = authority.None()
	case "policy":
		f.instance.policy = policy.Fixed(&policy.Set{})
	case "closed":
		if err := f.instance.Close(); err != nil {
			t.Fatal(err)
		}
	case "unsupported":
		f.instance.store = &unguardedReadStore{Store: f.store}
	}
	return ctx
}
