package fs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func saveStore(t *testing.T, dir string) *FilesystemStore {
	t.Helper()
	if !(&FilesystemStore{}).SupportsDurableSaveRequests() {
		t.Skip("durable saves require Darwin or Linux")
	}
	store, err := NewFilesystemStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func initializeSaveBucket(t *testing.T, store *FilesystemStore) {
	t.Helper()
	if err := store.CreateBucket(context.Background(), "saved"); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRecoveryAtPublicationBoundaries(t *testing.T) {
	for _, phase := range []string{"prepared", "publishing", "published", "sync", "terminal"} {
		t.Run(phase, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "nested", "owned")
			store := saveStore(t, dir)
			initializeSaveBucket(t, store)
			store.saveFault = func(current string) error {
				if current == phase {
					return errors.New("interrupted")
				}
				return nil
			}
			key := strings.Repeat("long-key/", 70)
			opts := storage.PutOptions{RequestKey: "retained-request", Guard: &storage.WriteGuard{Absent: true}, Metadata: map[string]string{"state": "saved"}}
			if _, err := store.PutObject(context.Background(), "saved", key, bytes.NewBufferString("payload"), opts); err == nil {
				t.Fatal("injected interruption was ignored")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := saveStore(t, dir)
			receipt, err := reopened.ResolveSaveRequest(nil, "saved", key, opts.RequestKey)
			assertRecoveredPhase(t, phase, receipt, err)
			if phase == "publishing" {
				assertPendingMutationGate(t, reopened)
				return
			}
			if err := reopened.CreateBucket(context.Background(), "after"); err != nil {
				t.Fatalf("terminal recovery blocked new work: %v", err)
			}
		})
	}
}

func assertRecoveredPhase(t *testing.T, phase string, receipt storage.SaveReceipt, err error) {
	t.Helper()
	switch phase {
	case "prepared":
		if !errors.Is(err, storage.ErrSaveRequestNotCommitted) || receipt.Outcome != "not_committed" || receipt.Meta != nil {
			t.Fatalf("prepared=%+v, %v", receipt, err)
		}
	case "publishing":
		if !errors.Is(err, storage.ErrSaveRequestUnknown) || receipt.Outcome != "unknown" {
			t.Fatalf("publishing=%+v, %v", receipt, err)
		}
	default:
		if err != nil || receipt.Outcome != "committed" || receipt.Meta == nil || receipt.Meta.Metadata["state"] != "saved" {
			t.Fatalf("published=%+v, %v", receipt, err)
		}
	}
}

func assertPendingMutationGate(t *testing.T, store *FilesystemStore) {
	t.Helper()
	ctx := context.Background()
	operations := []func() error{
		func() error { return store.CreateBucket(ctx, "blocked") },
		func() error { return store.DeleteBucket(ctx, "saved") },
		func() error {
			_, err := store.PutObject(ctx, "saved", "other", bytes.NewBufferString("x"), storage.PutOptions{})
			return err
		},
		func() error { return store.DeleteObject(ctx, "saved", "other") },
		func() error { _, err := store.DeleteObjects(ctx, "saved", []string{"other"}); return err },
		func() error { _, err := store.CopyObject(ctx, "saved", "other", "saved", "copy"); return err },
		func() error {
			_, err := store.CreateMultipartUpload(ctx, "saved", "part", storage.MultipartOptions{})
			return err
		},
		func() error { _, err := store.UploadPart(ctx, "upload", 1, bytes.NewBufferString("x")); return err },
		func() error {
			_, err := store.CompleteMultipartUpload(ctx, "upload", []storage.PartInfo{{PartNumber: 1, ETag: "part"}})
			return err
		},
		func() error { return store.AbortMultipartUpload(ctx, "upload") },
	}
	for index, operation := range operations {
		if err := operation(); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Errorf("mutation %d bypassed unknown: %v", index, err)
		}
	}
	if _, err := os.Stat(store.pendingPath()); err != nil {
		t.Fatal("pending evidence was removed")
	}
}

func TestSaveReplayRetainsOriginalMetadataAfterOverwriteAndDelete(t *testing.T) {
	store := saveStore(t, t.TempDir())
	initializeSaveBucket(t, store)
	ctx := context.Background()
	opts := storage.PutOptions{RequestKey: "first-save", Guard: &storage.WriteGuard{Absent: true}, ContentType: "text/plain", Metadata: map[string]string{"state": "first"}}
	original, err := store.PutObject(ctx, "saved", "report", bytes.NewBufferString("first"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutObject(ctx, "saved", "report", bytes.NewBufferString("later"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	receipt, found, err := store.ReplaySaveRequest(nil, "saved", "report", []byte("first"), opts)
	if err != nil || !found || receipt.Meta.VersionID != original.VersionID {
		t.Fatalf("replay=%+v,%v,%v", receipt, found, err)
	}
	current, _ := store.HeadObject(ctx, "saved", "report")
	if current.VersionID == original.VersionID {
		t.Fatal("replay overwrote newer object")
	}
	assertDeletedReceipt(t, store, original, opts)
}

func assertDeletedReceipt(t *testing.T, store *FilesystemStore, original *storage.ObjectMeta, opts storage.PutOptions) {
	t.Helper()
	ctx := context.Background()
	if err := store.DeleteObject(ctx, "saved", "report"); err != nil {
		t.Fatal(err)
	}
	receipt, err := store.ResolveSaveRequest(nil, "saved", "report", opts.RequestKey)
	if err != nil || receipt.Meta.VersionID != original.VersionID {
		t.Fatalf("deleted resolve=%+v,%v", receipt, err)
	}
	if _, err := store.HeadObject(ctx, "saved", "report"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("resolver restored deleted bytes: %v", err)
	}
	_, _, err = store.ReplaySaveRequest(ctx, "saved", "report", []byte("different"), opts)
	if !errors.Is(err, storage.ErrSaveRequestConflict) {
		t.Fatalf("changed payload=%v", err)
	}
	if _, err := store.ResolveSaveRequest(ctx, "saved", "other", opts.RequestKey); !errors.Is(err, storage.ErrSaveRequestConflict) {
		t.Fatalf("different resource=%v", err)
	}
}

func TestSaveJournalBoundedAdmission(t *testing.T) {
	for _, limit := range []string{"count", "bytes", "entry"} {
		t.Run(limit, func(t *testing.T) {
			store := saveStore(t, t.TempDir())
			initializeSaveBucket(t, store)
			opts := storage.PutOptions{RequestKey: "bounded", Guard: &storage.WriteGuard{Absent: true}}
			switch limit {
			case "count":
				for index := range maxSaveEntries {
					store.saves.entries[string(rune(index))] = saveEntry{}
				}
			case "bytes":
				store.saves.bytes = maxSaveJournalBytes
			case "entry":
				opts.Metadata = map[string]string{"large": strings.Repeat("x", maxSaveEntryBytes)}
			}
			_, err := store.PutObject(context.Background(), "saved", "new", bytes.NewBufferString("x"), opts)
			if !errors.Is(err, storage.ErrSaveRequestFull) {
				t.Fatalf("limit=%v", err)
			}
			if _, err := os.Stat(store.pendingPath()); !os.IsNotExist(err) {
				t.Fatal("refusal persisted pending intent")
			}
			if _, err := store.HeadObject(context.Background(), "saved", "new"); !errors.Is(err, storage.ErrObjectNotFound) {
				t.Fatal("refusal published object")
			}
		})
	}
}

func TestSavePendingCorruptionAndIdentityLossPreserveEvidence(t *testing.T) {
	for _, corruption := range []string{"malformed", "identity", "conflicting-terminal"} {
		t.Run(corruption, func(t *testing.T) {
			dir := t.TempDir()
			store := saveStore(t, dir)
			initializeSaveBucket(t, store)
			store.saveFault = func(phase string) error {
				if phase == "publishing" {
					return errors.New("stop")
				}
				return nil
			}
			_, _ = store.PutObject(context.Background(), "saved", "report", bytes.NewBufferString("x"), storage.PutOptions{RequestKey: "pending", Guard: &storage.WriteGuard{Absent: true}})
			pendingPath := store.pendingPath()
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			corruptSaveEvidence(t, store, corruption)
			reopened, err := NewFilesystemStore(dir)
			if err == nil {
				defer reopened.Close()
				assertPendingMutationGate(t, reopened)
			} else if !errors.Is(err, storage.ErrSaveRequestUnknown) {
				t.Fatalf("corruption=%v", err)
			}
			if _, err := os.Stat(pendingPath); err != nil {
				t.Fatal("corrupt recovery evidence removed")
			}
			if corruption == "identity" {
				if _, err := os.Stat(filepath.Join(dir, ".save-identity.json")); !os.IsNotExist(err) {
					t.Fatal("lost identity silently replaced")
				}
			}
		})
	}
}

func corruptSaveEvidence(t *testing.T, store *FilesystemStore, corruption string) {
	t.Helper()
	switch corruption {
	case "malformed":
		if err := os.WriteFile(store.pendingPath(), []byte("{broken"), 0o600); err != nil {
			t.Fatal(err)
		}
	case "identity":
		if err := os.Remove(filepath.Join(store.dataDir, ".save-identity.json")); err != nil {
			t.Fatal(err)
		}
	case "conflicting-terminal":
		entry := *store.saves.pending
		entry.Meaning = strings.Repeat("0", 64)
		entry.Phase = "committed"
		data, _ := encodeSaveEntry(entry)
		if err := os.WriteFile(filepath.Join(store.saveDir(), requestName(entry.RequestKey)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSaveEvidenceMetadataCorruptionRefuses(t *testing.T) {
	for _, kind := range []string{"pending-raw", "pending-valid", "terminal-raw"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			store := saveStore(t, dir)
			initializeSaveBucket(t, store)
			if kind != "terminal-raw" {
				store.saveFault = func(phase string) error {
					if phase == "published" {
						return errors.New("stop")
					}
					return nil
				}
			}
			_, err := store.PutObject(context.Background(), "saved", "report", bytes.NewBufferString("payload"), storage.PutOptions{RequestKey: "corrupt", Guard: &storage.WriteGuard{Absent: true}})
			if kind == "terminal-raw" && err != nil {
				t.Fatal(err)
			}
			path := store.pendingPath()
			if kind == "terminal-raw" {
				path = filepath.Join(store.saveDir(), requestName("corrupt"))
			}
			mutateSaveMetadata(t, path, kind == "pending-valid")
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewFilesystemStore(dir)
			if err == nil {
				defer reopened.Close()
				receipt, resolveErr := reopened.ResolveSaveRequest(nil, "saved", "report", "corrupt")
				if !errors.Is(resolveErr, storage.ErrSaveRequestUnknown) || receipt.Outcome != "unknown" {
					t.Fatalf("corrupt metadata=%+v,%v", receipt, resolveErr)
				}
			} else if !errors.Is(err, storage.ErrSaveRequestUnknown) {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("corrupt evidence was removed")
			}
		})
	}
}

func mutateSaveMetadata(t *testing.T, path string, valid bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entry saveEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	entry.Meta.ContentType = "corrupted"
	if valid {
		data, err = encodeSaveEntry(entry)
	} else {
		data, err = json.Marshal(entry)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSaveLegacyRecordAndUnkeyedPublication(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "legacy", "nested")
	store := saveStore(t, dir)
	initializeSaveBucket(t, store)
	ctx := context.Background()
	if _, err := store.PutObject(ctx, "saved", "old", bytes.NewBufferString("legacy"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	record, err := store.readObject("saved", "old")
	if err != nil {
		t.Fatal(err)
	}
	record.RecordVersion = ""
	if err := writeObjectRecord(store.objectPath("saved", "old"), record); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := saveStore(t, dir)
	reader, meta, err := reopened.GetObject(ctx, "saved", "old")
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	guard := storage.WriteGuard{Fingerprint: storage.ObjectFingerprint(*meta, []byte("legacy"))}
	saved, err := reopened.PutObject(ctx, "saved", "old", bytes.NewBufferString("modern"), storage.PutOptions{Guard: &guard})
	if err != nil || saved.VersionID == meta.VersionID {
		t.Fatalf("legacy guarded migration=%+v,%v", saved, err)
	}
	if _, err := reopened.PutObject(ctx, "saved", strings.Repeat("new-shard/", 60), bytes.NewBufferString("new"), storage.PutOptions{Guard: &storage.WriteGuard{Absent: true}}); err != nil {
		t.Fatal(err)
	}
}

func TestSaveInvalidRequestKeyRefusesAndReopens(t *testing.T) {
	dir := t.TempDir()
	store := saveStore(t, dir)
	initializeSaveBucket(t, store)
	requestKey := string([]byte{'r', 0xff})
	opts := storage.PutOptions{RequestKey: requestKey, Guard: &storage.WriteGuard{Absent: true}}
	if _, err := store.PutObject(context.Background(), "saved", "new", bytes.NewBufferString("x"), opts); !errors.Is(err, storage.ErrInvalidSaveRequest) {
		t.Fatalf("invalid key=%v", err)
	}
	if _, err := os.Stat(store.pendingPath()); !os.IsNotExist(err) {
		t.Fatal("invalid request created evidence")
	}
	entries, err := os.ReadDir(store.saveDir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("retained invalid request=%v, %v", entries, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := saveStore(t, dir)
	if _, err := reopened.HeadObject(context.Background(), "saved", "new"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("invalid request published bytes: %v", err)
	}
	if err := reopened.CreateBucket(context.Background(), "after"); err != nil {
		t.Fatalf("invalid request blocked reopen: %v", err)
	}
}

func TestSaveMalformedLogicalOptionsRefuseBeforeEvidence(t *testing.T) {
	malformed := string([]byte{0xff})
	for _, options := range []storage.PutOptions{
		{ContentType: malformed},
		{Metadata: map[string]string{malformed: "value"}},
		{Metadata: map[string]string{"key": malformed}},
		{ChecksumAlgorithm: malformed},
		{ChecksumValue: malformed},
	} {
		store := saveStore(t, t.TempDir())
		initializeSaveBucket(t, store)
		options.RequestKey = "invalid-options"
		options.Guard = &storage.WriteGuard{Absent: true}
		_, err := store.PutObject(context.Background(), "saved", "new", bytes.NewBufferString("x"), options)
		if !errors.Is(err, storage.ErrInvalidSaveRequest) {
			t.Fatalf("malformed options=%v", err)
		}
		if _, err := os.Stat(store.pendingPath()); !os.IsNotExist(err) {
			t.Fatal("malformed options created pending evidence")
		}
		if _, err := store.HeadObject(context.Background(), "saved", "new"); !errors.Is(err, storage.ErrObjectNotFound) {
			t.Fatal("malformed options published bytes")
		}
	}
}

func TestSaveMalformedLookupResourceRefuses(t *testing.T) {
	store := saveStore(t, t.TempDir())
	initializeSaveBucket(t, store)
	malformed := string([]byte{0xff})
	for _, resource := range [][2]string{{malformed, "key"}, {"saved", malformed}} {
		opts := storage.PutOptions{RequestKey: "request", Guard: &storage.WriteGuard{Absent: true}}
		_, _, err := store.ReplaySaveRequest(nil, resource[0], resource[1], []byte("x"), opts)
		if !errors.Is(err, storage.ErrInvalidSaveRequest) {
			t.Fatalf("invalid replay resource=%v", err)
		}
		receipt, err := store.ResolveSaveRequest(nil, resource[0], resource[1], "request")
		if !errors.Is(err, storage.ErrInvalidSaveRequest) || receipt.Meta != nil {
			t.Fatalf("invalid resolve resource=%+v,%v", receipt, err)
		}
	}
	if _, err := os.Stat(store.pendingPath()); !os.IsNotExist(err) {
		t.Fatal("invalid lookup created evidence")
	}
}
