package embedded

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
)

func TestEmbeddedBufferedReadRejectsPrematureEOF(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		for _, together := range []bool{false, true} {
			t.Run(fmt.Sprintf("guarded=%t/together=%t", guarded, together), func(t *testing.T) {
				checkEmbeddedPrematureEOF(t, guarded, together)
			})
		}
	}
}

func checkEmbeddedPrematureEOF(t *testing.T, guarded, together bool) {
	t.Helper()
	ctx := context.Background()
	store, outer := embeddedSaveBodyFixture(t)
	store.bodyErr, store.together = io.EOF, together
	var object Object
	var condition SaveCondition
	var err error
	if guarded {
		object, condition, err = outer.ReadForSave(ctx, "probe", "target")
	} else {
		object, err = outer.GetObject(ctx, "probe", "target")
	}
	if err != io.ErrUnexpectedEOF || !reflect.ValueOf(object).IsZero() || condition != (SaveCondition{}) {
		t.Errorf("public incomplete read became observation: object=%+v condition=%+v error=%v", object, condition, err)
	}
	if store.bytes != 1 || store.closes != 1 {
		t.Fatalf("public incomplete reader bytes=%d closes=%d", store.bytes, store.closes)
	}
	store.bodyErr = nil
	result, err := outer.SaveObject(ctx, "probe", "target", []byte("bad"), SaveOptions{Condition: condition})
	if !errors.Is(err, ErrInvalidSaveCondition) || result.Outcome != SaveNotCommitted {
		t.Errorf("public incomplete read issued usable condition: result=%+v error=%v", result, err)
	}
	object, condition, err = outer.ReadForSave(ctx, "probe", "target")
	if err != nil || string(object.Data) != "old" || condition.ObservedAbsence() {
		t.Fatalf("healthy public retry: object=%+v condition=%+v error=%v", object, condition, err)
	}
	if result, err := outer.SaveObject(ctx, "probe", "target", []byte("12345678"), SaveOptions{Condition: condition}); err != nil || result.Outcome != SaveCommitted {
		t.Fatalf("healthy public save: result=%+v error=%v", result, err)
	}
}
