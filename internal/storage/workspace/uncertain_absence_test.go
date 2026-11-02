package workspace_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	workspace "github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// "I cannot tell" is not "it is not there". A file the host will not let the
// store read has not answered the question, and the answer that loses data is
// the one that calls the object absent: a create-only write over it then
// replaces bytes that are still there.
//
// The reproduction is a real permission change on a real workspace file. It
// uses no custom store, no mocked absence error and no production hook, so the
// only thing under test is the store's own answer to a path it cannot read.
func TestACreateOnlyWriteDoesNotReplaceAnObjectItCannotRead(t *testing.T) {
	skipUnlessTheHostCanHideAnObject(t)
	root, store := workspaceWithObject(t, "stow-uncertain-bucket", "victim", []byte("old"))
	defer store.Close()
	ctx := context.Background()

	victim := filepath.Join(root, "victim")
	chmod(t, victim, 0o000)
	t.Cleanup(func() { _ = os.Chmod(victim, 0o600) })

	// A read that cannot be answered must not be reported as absence.
	_, headErr := store.HeadObject(ctx, "stow-uncertain-bucket", "victim")
	requireUnknownState(t, "HeadObject", headErr)
	reader, _, getErr := store.GetObject(ctx, "stow-uncertain-bucket", "victim")
	if reader != nil {
		reader.Close()
	}
	requireUnknownState(t, "GetObject", getErr)

	// The create-only write must be refused rather than admitted over bytes the
	// store could not read.
	_, putErr := store.PutObject(ctx, "stow-uncertain-bucket", "victim", bytes.NewReader([]byte("new")),
		storage.PutOptions{IfNoneMatch: "*"})
	if putErr == nil {
		t.Fatal("a create-only write over an unreadable object succeeded")
	}
	if isAbsent(putErr) {
		t.Errorf("a create-only write over an unreadable object = %v, want a refusal, not an absent object", putErr)
	}

	// And the bytes are still there once the host can read them again.
	chmod(t, victim, 0o600)
	requireOriginalBytes(t, victim, "old")
}

// A listing that cannot read an object must not read as a listing without it.
// A caller that sees the object missing concludes its work was deleted.
func TestAListingDoesNotReportAnObjectItCannotReadAsAbsent(t *testing.T) {
	skipUnlessTheHostCanHideAnObject(t)
	root, store := workspaceWithObject(t, "stow-uncertain-bucket", "victim", []byte("old"))
	defer store.Close()
	ctx := context.Background()

	victim := filepath.Join(root, "victim")
	chmod(t, victim, 0o000)
	t.Cleanup(func() { _ = os.Chmod(victim, 0o600) })

	listed, err := store.ListObjectsV2(ctx, "stow-uncertain-bucket", storage.ListOptions{})
	if err == nil {
		for _, object := range listed.Objects {
			if object.Key == "victim" {
				t.Errorf("the listing reported an unreadable object at %d bytes, which no read verified", object.Size)
			}
		}
		t.Fatal("a listing that could not read an object succeeded, so a caller would conclude the object was gone")
	}
	if isAbsent(err) {
		t.Errorf("a listing that could not read an object = %v, want the failure reported, not an empty listing", err)
	}
}

// A directory that cannot be listed is not an empty directory, so the same rule
// applies one level up: a key under it is unknown rather than absent.
func TestACreateOnlyWriteUnderAnUnreadableDirectoryIsRefused(t *testing.T) {
	skipUnlessTheHostCanHideAnObject(t)
	root, store := workspaceWithObject(t, "stow-uncertain-bucket", "dir/victim", []byte("old"))
	defer store.Close()
	ctx := context.Background()

	directory := filepath.Join(root, "dir")
	chmod(t, directory, 0o000)
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })

	_, headErr := store.HeadObject(ctx, "stow-uncertain-bucket", "dir/victim")
	requireUnknownState(t, "HeadObject", headErr)
	if _, err := store.PutObject(ctx, "stow-uncertain-bucket", "dir/victim", bytes.NewReader([]byte("new")),
		storage.PutOptions{IfNoneMatch: "*"}); err == nil {
		t.Error("a create-only write under an unreadable directory was admitted")
	}

	chmod(t, directory, 0o700)
	requireOriginalBytes(t, filepath.Join(directory, "victim"), "old")
}

// The healthy paths still answer. A readable object refuses a create-only
// write, and a key that is genuinely absent is absent and can be created.
func TestAReadableObjectStillRefusesACreateOnlyWriteAndAnAbsentKeyIsAbsent(t *testing.T) {
	const bucket = "stow-uncertain-bucket"
	root, store := workspaceWithObject(t, bucket, "victim", []byte("old"))
	defer store.Close()
	ctx := context.Background()

	if _, err := store.PutObject(ctx, bucket, "victim", bytes.NewReader([]byte("new")),
		storage.PutOptions{IfNoneMatch: "*"}); !errors.Is(err, storage.ErrPreconditionFailed) {
		t.Errorf("a create-only write over a readable object = %v, want storage.ErrPreconditionFailed", err)
	}
	requireOriginalBytes(t, filepath.Join(root, "victim"), "old")

	if _, err := store.HeadObject(ctx, bucket, "absent"); !isAbsent(err) {
		t.Errorf("HeadObject on an absent key = %v, want the key reported as absent", err)
	}
	if _, err := store.PutObject(ctx, bucket, "absent", bytes.NewReader([]byte("new")),
		storage.PutOptions{IfNoneMatch: "*"}); err != nil {
		t.Errorf("a create-only write to an absent key = %v, want the object created", err)
	}
}

// A root process can read a file with no permissions, so the case cannot be
// arranged there.
func skipUnlessTheHostCanHideAnObject(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can read a file with no permissions, so this cannot be arranged here")
	}
}

// A failure that leaves the object's state unknown, rather than a confirmed
// absence.
func requireUnknownState(t *testing.T, operation string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s on an unreadable object succeeded, want the state reported as unknown", operation)
		return
	}
	if isAbsent(err) {
		t.Errorf("%s on an unreadable object = %v, want the state reported as unknown, not as absence", operation, err)
		return
	}
	if !errors.Is(err, storage.ErrObjectUncertain) {
		t.Errorf("%s on an unreadable object = %v, want storage.ErrObjectUncertain", operation, err)
		return
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("%s on an unreadable object = %v, want the permission cause kept", operation, err)
	}
}

func requireOriginalBytes(t *testing.T, path, want string) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the object back: %v", err)
	}
	if string(after) != want {
		t.Errorf("the object now holds %q, want the original bytes %q", after, want)
	}
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

// A workspace holding one object with known bytes.
func workspaceWithObject(t *testing.T, bucket, key string, data []byte) (string, *workspace.Store) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ws")
	store := openStore(t, root, bucket)
	if _, err := store.PutObject(context.Background(), bucket, key, bytes.NewReader(data), storage.PutOptions{}); err != nil {
		t.Fatalf("write %s: %v", key, err)
	}
	return root, store
}
