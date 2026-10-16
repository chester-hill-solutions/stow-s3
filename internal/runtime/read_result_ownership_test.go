package runtime

import (
	"context"
	"fmt"
	"testing"
)

func TestBufferedReadsOwnReturnedState(t *testing.T) {
	for _, firstGuarded := range []bool{false, true} {
		for _, secondGuarded := range []bool{false, true} {
			t.Run(fmt.Sprintf("first-guarded=%t/second-guarded=%t", firstGuarded, secondGuarded), func(t *testing.T) {
				checkBufferedReadOwnership(t, firstGuarded, secondGuarded)
			})
		}
	}
}

func ownedReadObservation(t *testing.T, instance *Instance, guarded bool) (Object, SaveCondition) {
	t.Helper()
	ctx := context.Background()
	if guarded {
		object, condition, err := instance.ReadForSave(ctx, "probe", "target")
		if err != nil || condition.ObservedAbsence() {
			t.Fatalf("guarded observation: object=%+v absent=%t error=%v", object, condition.ObservedAbsence(), err)
		}
		return object, condition
	}
	object, err := instance.GetObject(ctx, "probe", "target")
	if err != nil {
		t.Fatal(err)
	}
	return object, SaveCondition{}
}

func checkBufferedReadOwnership(t *testing.T, firstGuarded, secondGuarded bool) {
	t.Helper()
	f := newSaveReadFixture(t, true)
	first, firstCondition := ownedReadObservation(t, f.instance, firstGuarded)
	second, _ := ownedReadObservation(t, f.instance, secondGuarded)
	if len(first.Data) != 3 || first.Metadata == nil {
		t.Fatalf("incomplete first result: %+v", first)
	}
	first.Data[0] = 'x'
	first.Metadata["state"] = "caller-changed"
	if string(second.Data) != "old" || second.Metadata["state"] != "original" || second.VersionID != f.before.VersionID {
		t.Fatalf("read results alias: %+v", second)
	}
	fresh, condition := ownedReadObservation(t, f.instance, true)
	if string(fresh.Data) != "old" || fresh.Metadata["state"] != "original" || fresh.VersionID != f.before.VersionID {
		t.Fatalf("caller mutation changed storage: %+v", fresh)
	}
	if firstGuarded {
		condition = firstCondition
	}
	result, err := f.instance.SaveObject(context.Background(), "probe", "target", []byte("12345678"), SaveOptions{Condition: condition})
	if err != nil || result.Outcome != SaveCommitted || f.instance.Usage() != (Usage{Bytes: 8, Objects: 1}) {
		t.Fatalf("caller mutation changed guard/capacity: result=%+v error=%v usage=%+v", result, err, f.instance.Usage())
	}
}
