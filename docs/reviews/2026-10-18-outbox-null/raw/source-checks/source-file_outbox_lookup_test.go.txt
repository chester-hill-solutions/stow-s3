package runthrough_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

func newLookupOutbox(t *testing.T) (*runthrough.FileOutbox, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "outbox")
	outbox, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	return outbox, path
}

func requireLookupValue(t *testing.T, outbox *runthrough.FileOutbox, want runthrough.UpstreamState) {
	t.Helper()
	got, known, err := outbox.UpstreamState("bucket", "key")
	if err != nil || !known || got != want {
		t.Fatalf("value=%+v known=%t error=%v, want %+v", got, known, err, want)
	}
}

func TestFileOutboxLookupReloadsSharedProvenance(t *testing.T) {
	writer, path := newLookupOutbox(t)
	reader, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if value, known, err := reader.UpstreamState("bucket", "key"); err != nil || known || value != (runthrough.UpstreamState{}) {
		t.Fatalf("missing value=%+v known=%t error=%v", value, known, err)
	}
	for _, want := range []runthrough.UpstreamState{{ETag: "first"}, {Absent: true}, {Unknown: true}, {}, {ETag: "last"}} {
		if err := writer.SaveUpstreamState("bucket", "key", want); err != nil {
			t.Fatal(err)
		}
		requireLookupValue(t, reader, want)
		if value, known, err := reader.UpstreamState("other-bucket", "key"); err != nil || known || value != (runthrough.UpstreamState{}) {
			t.Fatalf("other bucket value=%+v known=%t error=%v", value, known, err)
		}
	}
}

func applyLookupFault(t *testing.T, path, fault string) func() {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fault == "corrupt-journal" {
		if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
			t.Fatal(err)
		}
		return func() {
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	broken := path
	if fault == "unavailable-lock" {
		broken += ".lock"
	}
	if err := os.Remove(broken); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(broken, 0o700); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Remove(broken); err != nil {
			t.Fatal(err)
		}
		if fault != "unavailable-lock" {
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestFileOutboxLookupFailsClosedAndRecovers(t *testing.T) {
	for _, fault := range []string{"corrupt-journal", "unreadable-journal", "unavailable-lock"} {
		t.Run(fault, func(t *testing.T) {
			outbox, path := newLookupOutbox(t)
			want := runthrough.UpstreamState{ETag: "committed"}
			if err := outbox.SaveUpstreamState("bucket", "key", want); err != nil {
				t.Fatal(err)
			}
			repair := applyLookupFault(t, path, fault)
			for n := 0; n < 2; n++ {
				if got, known, err := outbox.UpstreamState("bucket", "key"); err == nil || known || got != (runthrough.UpstreamState{}) {
					t.Fatalf("failed lookup returned stale state: value=%+v known=%t error=%v", got, known, err)
				}
			}
			repair()
			requireLookupValue(t, outbox, want)
		})
	}
}

func writeLookupRepeatedly(outbox *runthrough.FileOutbox) error {
	for n := 0; n < 16; n++ {
		value := runthrough.UpstreamState{ETag: "one"}
		if n%2 == 0 {
			value.ETag = "two"
		}
		if err := outbox.SaveUpstreamState("bucket", "key", value); err != nil {
			return err
		}
	}
	return nil
}

func readLookupRepeatedly(outbox *runthrough.FileOutbox) error {
	for n := 0; n < 32; n++ {
		value, known, err := outbox.UpstreamState("bucket", "key")
		if err != nil || !known || (value.ETag != "one" && value.ETag != "two") || value.Absent || value.Unknown {
			return fmt.Errorf("concurrent lookup value=%+v known=%t error=%v", value, known, err)
		}
	}
	return nil
}

func TestFileOutboxLookupConcurrentReadersAndWriter(t *testing.T) {
	first, path := newLookupOutbox(t)
	second, err := runthrough.NewFileOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SaveUpstreamState("bucket", "key", runthrough.UpstreamState{ETag: "one"}); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	start := make(chan struct{})
	failures := make(chan error, 5)
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		if err := writeLookupRepeatedly(first); err != nil {
			failures <- err
		}
	}()
	for _, reader := range []*runthrough.FileOutbox{first, second, first, second} {
		workers.Add(1)
		go func(reader *runthrough.FileOutbox) {
			defer workers.Done()
			<-start
			if err := readLookupRepeatedly(reader); err != nil {
				failures <- err
			}
		}(reader)
	}
	close(start)
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}
