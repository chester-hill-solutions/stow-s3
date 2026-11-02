package runthrough

import (
	"os"
	"reflect"
	"testing"
)

func TestOutboxAdmissionRejectsExhaustedSequence(t *testing.T) {
	for _, backend := range []string{"memory", "file"} {
		t.Run(backend, func(t *testing.T) { checkExhaustedAdmission(t, backend) })
	}
}

func checkExhaustedAdmission(t *testing.T, backend string) {
	t.Helper()
	f := newAdmissionFixture(t, backend, "active")
	for _, id := range []string{"outbox-0", "outbox-18446744073709551615"} {
		if _, err := f.outbox.Enqueue(OutboxEntry{ID: id, Operation: OutboxPut, Bucket: "bucket", Key: id, Version: "protected"}); err != nil {
			t.Fatal(err)
		}
	}
	want := f.outbox.Pending()
	var data []byte
	if f.path != "" {
		var err error
		data, err = os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := f.outbox.Enqueue(OutboxEntry{Operation: OutboxDelete, Bucket: "bucket", Key: "wrapped-replacement"})
	if err == nil || got != (OutboxEntry{}) {
		t.Errorf("exhausted sequence admitted replacement: %+v %v", got, err)
	}
	if !reflect.DeepEqual(f.outbox.Pending(), want) {
		t.Error("sequence wrap replaced a live intent")
	}
	if f.path != "" {
		checkAdmissionFile(t, f.path, data)
	}
	if _, err := f.outbox.Enqueue(OutboxEntry{ID: "available-after-exhaustion", Operation: OutboxPut, Bucket: "bucket", Key: "available", Version: "available"}); err != nil {
		t.Fatal(err)
	}
}
