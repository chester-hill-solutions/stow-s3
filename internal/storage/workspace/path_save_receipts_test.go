package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestWorkspaceFileSaveReplayAndMeaningConflict(t *testing.T) {
	store := newFileSaveJournalStore(t)
	if _, err := store.PutObject(context.Background(), "bucket", "note.txt", bytes.NewReader([]byte("old")), storage.PutOptions{ContentType: "text/custom", Metadata: map[string]string{"owner": "host"}}); err != nil {
		t.Fatal(err)
	}
	request := newWorkspaceFileSaveRequest(t, store, "request-replay", "note.txt", []byte("saved"))
	assertWorkspaceFileSaveInitialResult(t, store, request)
	request.Write.Basis = nil
	assertWorkspaceFileSaveReceiptClone(t, store, request)
	assertWorkspaceFileSaveMeaningConflict(t, store, request)
}

func assertWorkspaceFileSaveInitialResult(t *testing.T, store *Store, request storage.WorkspaceFileSaveRequest) {
	t.Helper()
	receipt, err := store.SaveWorkspaceFile(context.Background(), "bucket", request)
	if err != nil || receipt.Outcome != workspaceFileSaveCommitted || receipt.Replayed || receipt.Meta == nil || receipt.Meta.Metadata["owner"] != "host" {
		t.Fatalf("save receipt=%+v error=%v", receipt, err)
	}
}

func assertWorkspaceFileSaveReceiptClone(t *testing.T, store *Store, request storage.WorkspaceFileSaveRequest) {
	t.Helper()
	replayed, found, err := store.ReplayWorkspaceFileSave(context.Background(), "bucket", request)
	if err != nil || !found || replayed.Outcome != workspaceFileSaveCommitted || !replayed.Replayed || replayed.Meta == nil {
		t.Fatalf("replay=%+v found=%v error=%v", replayed, found, err)
	}
	replayed.Meta.Key = "mutated"
	replayed.Meta.Metadata["owner"] = "mutated"
	again, found, err := store.ReplayWorkspaceFileSave(context.Background(), "bucket", request)
	if err != nil || !found || again.Meta == nil || again.Meta.Key != "note.txt" || again.Meta.Metadata["owner"] != "host" {
		t.Fatalf("receipt was not cloned: %+v found=%v error=%v", again, found, err)
	}
}

func assertWorkspaceFileSaveMeaningConflict(t *testing.T, store *Store, request storage.WorkspaceFileSaveRequest) {
	t.Helper()
	changed := request
	changed.Write.Data = []byte("changed meaning")
	if _, found, err := store.ReplayWorkspaceFileSave(context.Background(), "bucket", changed); !found || !errors.Is(err, storage.ErrSaveRequestConflict) {
		t.Fatalf("changed meaning replay found=%v error=%v", found, err)
	}
}

func TestWorkspaceFileSaveReplayAndResolveMissDoNotInitialize(t *testing.T) {
	store := newFileSaveJournalStore(t)
	request := newWorkspaceFileSaveRequest(t, store, "missing", "note.txt", []byte("saved"))
	if _, found, err := store.ReplayWorkspaceFileSave(context.Background(), "bucket", request); err != nil || found {
		t.Fatalf("missing replay found=%v error=%v", found, err)
	}
	if _, err := store.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", "missing"); !errors.Is(err, storage.ErrSaveRequestNotFound) {
		t.Fatalf("missing resolve=%v", err)
	}
	request.Write.Basis = nil
	if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); !errors.Is(err, storage.ErrInvalidSaveRequest) {
		t.Fatalf("fresh request without basis=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(store.root, workspaceFileSaveIdentityPath)); !os.IsNotExist(err) {
		t.Fatalf("replay miss initialized journal: %v", err)
	}
}

func TestWorkspaceFileSaveReplaceSkipsObservationBasis(t *testing.T) {
	store := newFileSaveJournalStore(t)
	if _, err := store.PutObject(context.Background(), "bucket", "note.txt", bytes.NewReader([]byte("old")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	request := storage.WorkspaceFileSaveRequest{RequestKey: "replace", Write: storage.WorkspaceFileWriteRequest{Path: "note.txt", Data: []byte("new"), Replace: true, MaxFileBytes: 64 << 10}}
	receipt, err := store.SaveWorkspaceFile(context.Background(), "bucket", request)
	if err != nil || receipt.Outcome != workspaceFileSaveCommitted {
		t.Fatalf("replace without basis=%+v error=%v", receipt, err)
	}
}

func TestWorkspaceFileSaveStaleAndConcurrentCompetitors(t *testing.T) {
	store := newFileSaveJournalStore(t)
	request := newWorkspaceFileSaveRequest(t, store, "stale", "note.txt", []byte("saved"))
	if err := os.WriteFile(filepath.Join(store.root, "note.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); !errors.Is(err, storage.ErrWorkspaceFileConflict) {
		t.Fatalf("stale basis=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(store.root, workspaceFileSaveIdentityPath)); !os.IsNotExist(err) {
		t.Fatalf("stale save initialized journal: %v", err)
	}

	second := newWorkspaceFileSaveStoreForTest(t)
	firstRequest := newWorkspaceFileSaveRequest(t, second, "first", "shared.txt", []byte("one"))
	other := firstRequest
	other.RequestKey = "second"
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, candidate := range []storage.WorkspaceFileSaveRequest{firstRequest, other} {
		group.Add(1)
		go func(value storage.WorkspaceFileSaveRequest) {
			defer group.Done()
			_, saveErr := second.SaveWorkspaceFile(context.Background(), "bucket", value)
			results <- saveErr
		}(candidate)
	}
	group.Wait()
	close(results)
	committed, conflicted := 0, 0
	for err := range results {
		if err == nil {
			committed++
		} else if errors.Is(err, storage.ErrWorkspaceFileConflict) {
			conflicted++
		} else {
			t.Fatalf("competitor error: %v", err)
		}
	}
	if committed != 1 || conflicted != 1 {
		t.Fatalf("committed=%d conflicted=%d", committed, conflicted)
	}
}

func TestWorkspaceFileSavePreparedAndPublishingResolution(t *testing.T) {
	preparedStore := newFileSaveJournalStore(t)
	prepared := newWorkspaceFileSaveRequest(t, preparedStore, "prepared", "note.txt", []byte("not written"))
	preparedStore.fileSavePhaseFault = func(phase string) error {
		if phase == workspaceFileSavePrepared {
			return errors.New("prepared fault")
		}
		return nil
	}
	if _, err := preparedStore.SaveWorkspaceFile(context.Background(), "bucket", prepared); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("prepared fault save=%v", err)
	}
	preparedStore.fileSavePhaseFault = nil
	receipt, err := preparedStore.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", prepared.RequestKey)
	if !errors.Is(err, storage.ErrSaveRequestNotCommitted) || receipt.Outcome != workspaceFileSaveNotCommitted {
		t.Fatalf("prepared resolution=%+v error=%v", receipt, err)
	}
	if _, err := os.Lstat(filepath.Join(preparedStore.root, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("prepared resolution wrote payload: %v", err)
	}

	publishingStore := newFileSaveJournalStore(t)
	publishing := newWorkspaceFileSaveRequest(t, publishingStore, "publishing", "note.txt", []byte("ambiguous"))
	publishingStore.fileSavePhaseFault = func(phase string) error {
		if phase == workspaceFileSavePublishing {
			return errors.New("publishing fault")
		}
		return nil
	}
	if _, err := publishingStore.SaveWorkspaceFile(context.Background(), "bucket", publishing); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("publishing fault save=%v", err)
	}
	publishingStore.fileSavePhaseFault = nil
	if _, err := publishingStore.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", publishing.RequestKey); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("publishing resolution=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(publishingStore.root, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("publishing fault wrote payload: %v", err)
	}
}

func TestWorkspaceFileSaveCancellationBeforeAndAfterPublishingAdmission(t *testing.T) {
	t.Run("prepared cancellation", func(t *testing.T) {
		store := newFileSaveJournalStore(t)
		request := newWorkspaceFileSaveRequest(t, store, "cancel-prepared", "note.txt", []byte("not written"))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		writerCalls := 0
		store.fileSavePayloadWriter = func(string, []byte, os.FileMode) error {
			writerCalls++
			return nil
		}
		store.fileSavePhaseFault = func(phase string) error {
			if phase == workspaceFileSavePrepared {
				cancel()
			}
			return nil
		}
		receipt, err := store.SaveWorkspaceFile(ctx, "bucket", request)
		if !errors.Is(err, context.Canceled) || !errors.Is(err, storage.ErrSaveRequestNotCommitted) || receipt.Outcome != workspaceFileSaveNotCommitted || writerCalls != 0 {
			t.Fatalf("canceled prepared save=%+v calls=%d error=%v", receipt, writerCalls, err)
		}
	})
	t.Run("publishing admitted", func(t *testing.T) {
		store := newFileSaveJournalStore(t)
		request := newWorkspaceFileSaveRequest(t, store, "cancel-publishing", "note.txt", []byte("finished"))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		store.fileSavePhaseFault = func(phase string) error {
			if phase == workspaceFileSavePublishing {
				cancel()
			}
			return nil
		}
		receipt, err := store.SaveWorkspaceFile(ctx, "bucket", request)
		if err != nil || receipt.Outcome != workspaceFileSaveCommitted {
			t.Fatalf("canceled admitted save=%+v error=%v", receipt, err)
		}
	})
}

func TestWorkspaceFileSavePublishedProofRecoversAndDoesNotRetryPayload(t *testing.T) {
	store := newFileSaveJournalStore(t)
	request := newWorkspaceFileSaveRequest(t, store, "published", "note.txt", []byte("saved"))
	writerCalls := 0
	store.fileSavePayloadWriter = func(path string, data []byte, mode os.FileMode) error {
		writerCalls++
		return store.writeAtomicMode(path, data, mode, false)
	}
	store.fileSavePhaseFault = func(phase string) error {
		if phase == workspaceFileSavePublished {
			return errors.New("after proof")
		}
		return nil
	}
	if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("published fault save=%v", err)
	}
	store.fileSavePhaseFault = nil
	receipt, err := store.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", request.RequestKey)
	if err != nil || receipt.Outcome != workspaceFileSaveCommitted || writerCalls != 1 {
		t.Fatalf("published resolution=%+v calls=%d error=%v", receipt, writerCalls, err)
	}
	if data, err := os.ReadFile(filepath.Join(store.root, "note.txt")); err != nil || !bytes.Equal(data, request.Write.Data) {
		t.Fatalf("resolved payload=%q error=%v", data, err)
	}
	if _, err := store.UsageRevision(context.Background()); err != nil {
		t.Fatalf("journal readiness after resolution=%v", err)
	}
	if _, err := store.ObserveWorkspaceFile(context.Background(), "bucket", "note.txt", 64<<10); err != nil {
		t.Fatalf("observation after resolution=%v", err)
	}
	second := newWorkspaceFileSaveRequest(t, store, "fresh", "note.txt", []byte("next"))
	second.Write.Basis.Data = nil
	if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", second); err != nil {
		t.Fatalf("fresh save after resolution=%v", err)
	}
}

func TestWorkspaceFileSaveIndexFailureResolvesWithoutPayloadRetry(t *testing.T) {
	store := newFileSaveJournalStore(t)
	request := newWorkspaceFileSaveRequest(t, store, "index-failure", "note.txt", []byte("committed bytes"))
	writerCalls := 0
	store.fileSavePayloadWriter = func(path string, data []byte, mode os.FileMode) error {
		writerCalls++
		return store.writeAtomicMode(path, data, mode, false)
	}
	index := filepath.Join(store.root, ".stow", "index.json")
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("index failure save=%v", err)
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	receipt, err := store.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", request.RequestKey)
	if err != nil || receipt.Outcome != workspaceFileSaveCommitted || writerCalls != 1 {
		t.Fatalf("index recovery=%+v calls=%d error=%v", receipt, writerCalls, err)
	}
	if entry, ok := store.objectIndex.entry("bucket", "note.txt"); !ok || entry.VersionID != receipt.Meta.VersionID {
		t.Fatalf("resolved index entry=%+v receipt=%+v", entry, receipt.Meta)
	}
}

func TestWorkspaceFileSavePublishedIndexTargetAndDivergence(t *testing.T) {
	for _, mode := range []string{"target", "divergent"} {
		t.Run(mode, func(t *testing.T) {
			store := newFileSaveJournalStore(t)
			request := newWorkspaceFileSaveRequest(t, store, "index-"+mode, "note.txt", []byte("saved"))
			writerCalls := 0
			store.fileSavePayloadWriter = func(path string, data []byte, fileMode os.FileMode) error {
				writerCalls++
				return store.writeAtomicMode(path, data, fileMode, false)
			}
			store.fileSavePhaseFault = func(phase string) error {
				if phase == workspaceFileSavePublished {
					return errors.New("published fault")
				}
				return nil
			}
			if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); !errors.Is(err, storage.ErrSaveRequestUnknown) {
				t.Fatalf("published save=%v", err)
			}
			target := store.fileSaveJournal.pending.PlannedEntry
			if mode == "divergent" {
				target.VersionID = "00000000000000000000000000000000"
			}
			store.objectIndex.setEntry("bucket", "note.txt", target)
			store.fileSavePhaseFault = nil
			receipt, err := store.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", request.RequestKey)
			if mode == "target" {
				if err != nil || receipt.Outcome != workspaceFileSaveCommitted || writerCalls != 1 {
					t.Fatalf("target index recovery=%+v calls=%d error=%v", receipt, writerCalls, err)
				}
				return
			}
			if !errors.Is(err, storage.ErrSaveRequestUnknown) || receipt.Outcome != "unknown" || writerCalls != 1 {
				t.Fatalf("divergent index result=%+v calls=%d error=%v", receipt, writerCalls, err)
			}
		})
	}
}

func TestWorkspaceFileSavePayloadWriterErrorRemainsPublishingUnknown(t *testing.T) {
	store := newFileSaveJournalStore(t)
	request := newWorkspaceFileSaveRequest(t, store, "write-unknown", "note.txt", []byte("written but unknown"))
	writerCalls := 0
	store.fileSavePayloadWriter = func(path string, data []byte, mode os.FileMode) error {
		writerCalls++
		if err := store.writeAtomicMode(path, data, mode, false); err != nil {
			return err
		}
		return errors.New("sync result was ambiguous")
	}
	if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("ambiguous writer error=%v", err)
	}
	if _, err := store.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", request.RequestKey); !errors.Is(err, storage.ErrSaveRequestUnknown) {
		t.Fatalf("publishing resolution=%v", err)
	}
	if writerCalls != 1 {
		t.Fatalf("ambiguous writer retried %d times", writerCalls)
	}
	if _, err := store.ObserveWorkspaceFile(context.Background(), "bucket", "note.txt", 64<<10); !errors.Is(err, storage.ErrWorkspaceFileBlocked) {
		t.Fatalf("observation during publishing=%v", err)
	}
}

func TestWorkspaceFileSaveFileMismatchAndStickyC2Block(t *testing.T) {
	t.Run("file mismatch", func(t *testing.T) {
		store := newFileSaveJournalStore(t)
		request := newWorkspaceFileSaveRequest(t, store, "file-mismatch", "note.txt", []byte("saved"))
		store.fileSavePhaseFault = func(phase string) error {
			if phase == workspaceFileSavePublished {
				return errors.New("pause after proof")
			}
			return nil
		}
		if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("published fault=%v", err)
		}
		store.fileSavePhaseFault = nil
		if err := os.WriteFile(filepath.Join(store.root, "note.txt"), []byte("replaced"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", request.RequestKey); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("mismatched published file resolution=%v", err)
		}
	})
	t.Run("root mismatch", func(t *testing.T) {
		store := newFileSaveJournalStore(t)
		request := newWorkspaceFileSaveRequest(t, store, "root-mismatch", "note.txt", []byte("saved"))
		store.fileSavePhaseFault = func(phase string) error {
			if phase == workspaceFileSavePublished {
				return errors.New("pause after proof")
			}
			return nil
		}
		if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("published fault=%v", err)
		}
		moved := store.root + "-moved"
		if err := os.Rename(store.root, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(store.root, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", request.RequestKey); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("root replacement resolution=%v", err)
		}
	})
	t.Run("C2 block", func(t *testing.T) {
		store := newFileSaveJournalStore(t)
		request := newWorkspaceFileSaveRequest(t, store, "sticky", "note.txt", []byte("saved"))
		store.fileSavePhaseFault = func(phase string) error {
			if phase == workspaceFileSavePublished {
				return errors.New("pause after proof")
			}
			return nil
		}
		if _, err := store.SaveWorkspaceFile(context.Background(), "bucket", request); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("published fault=%v", err)
		}
		store.fileSavePhaseFault = nil
		store.mu.Lock()
		store.markWorkspaceFileBlockedLocked()
		store.mu.Unlock()
		if _, err := store.ResolveWorkspaceFileSave(context.Background(), "bucket", "note.txt", request.RequestKey); err != nil {
			t.Fatalf("resolve with sticky C2 block=%v", err)
		}
		if _, err := store.UsageRevision(context.Background()); !errors.Is(err, storage.ErrSaveRequestUnknown) {
			t.Fatalf("journal refresh cleared C2 block: %v", err)
		}
	})
}

func TestWorkspaceFileSaveTerminalAndCleanupFaultKeepKnownReceipt(t *testing.T) {
	for _, phase := range []string{"terminal", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			store := newFileSaveJournalStore(t)
			request := newWorkspaceFileSaveRequest(t, store, "known-"+phase, "note.txt", []byte("committed"))
			writerCalls := 0
			store.fileSavePayloadWriter = func(path string, data []byte, mode os.FileMode) error {
				writerCalls++
				return store.writeAtomicMode(path, data, mode, false)
			}
			store.fileSavePhaseFault = func(current string) error {
				if current == phase {
					return errors.New("terminal cleanup fault")
				}
				return nil
			}
			receipt, err := store.SaveWorkspaceFile(context.Background(), "bucket", request)
			if receipt.Outcome != workspaceFileSaveCommitted || receipt.Meta == nil || !errors.Is(err, storage.ErrMutationCommitted) {
				t.Fatalf("save receipt=%+v error=%v", receipt, err)
			}
			store.fileSavePhaseFault = nil
			replayed, found, replayErr := store.ReplayWorkspaceFileSave(context.Background(), "bucket", request)
			if replayErr != nil || !found || replayed.Outcome != workspaceFileSaveCommitted || writerCalls != 1 {
				t.Fatalf("terminal replay=%+v found=%v calls=%d error=%v", replayed, found, writerCalls, replayErr)
			}
		})
	}
}

func TestOrdinaryWorkspaceNewRefusesFileSaveMarkersBeforeMutation(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		marker string
		link   bool
	}{{name: "exact", marker: "file-saves"}, {name: "alias", marker: "File-Saves"}, {name: "symlink", marker: "file-saves", link: true}} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".stow"), 0o700); err != nil {
				t.Fatal(err)
			}
			markerPath := filepath.Join(root, ".stow", testCase.marker)
			if testCase.link {
				target := t.TempDir()
				if err := os.Symlink(target, markerPath); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(markerPath, 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := New(Options{Root: root, Bucket: "bucket"}); !errors.Is(err, storage.ErrWorkspaceFileUnsupported) {
				t.Fatalf("New with marker=%q: %v", testCase.marker, err)
			}
			for _, name := range []string{"keys", "manifest.json", "index.json"} {
				if _, err := os.Lstat(filepath.Join(root, ".stow", name)); !os.IsNotExist(err) {
					t.Fatalf("New mutated %s before refusing marker: %v", name, err)
				}
			}
		})
	}
}

func newWorkspaceFileSaveStoreForTest(t *testing.T) *Store {
	t.Helper()
	store := newFileSaveJournalStore(t)
	return store
}

func newWorkspaceFileSaveRequest(t *testing.T, store *Store, requestKey, path string, data []byte) storage.WorkspaceFileSaveRequest {
	t.Helper()
	basis, err := store.ObserveWorkspaceFile(context.Background(), "bucket", path, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	basis.Data = nil
	token := sha256.Sum256([]byte("opaque observation token " + requestKey))
	return storage.WorkspaceFileSaveRequest{RequestKey: requestKey, BasisTokenDigest: hex.EncodeToString(token[:]),
		Write: storage.WorkspaceFileWriteRequest{Path: path, Data: data, Basis: &basis, MaxFileBytes: 64 << 10}}
}
