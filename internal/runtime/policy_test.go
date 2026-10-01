package runtime_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// openPolicy builds an environment whose authority is the full set and whose
// policy is the given one. The policy narrows a full authority rather than
// replacing one, because that is the case it exists for: a caller that may do
// anything in the environment, confined to part of it.
func openPolicy(t *testing.T, build func(*policy.Set)) *runtime.Instance {
	t.Helper()
	store := storage.NewMemoryStore()
	ctx := context.Background()

	seeded := map[string][]string{
		"public":  {"readme", "public/page", "publicish", "public/secret", "public/a"},
		"private": {"readme"},
	}
	for bucket, keys := range seeded {
		if err := store.CreateBucket(ctx, bucket); err != nil {
			t.Fatalf("seed bucket: %v", err)
		}
		for _, key := range keys {
			if _, err := store.PutObject(ctx, bucket, key, strings.NewReader("hello"), storage.PutOptions{}); err != nil {
				t.Fatalf("seed object: %v", err)
			}
		}
	}
	var set policy.Set
	build(&set)
	instance, err := runtime.OpenWithStore(runtime.Options{
		Backend: runtime.BackendMemory,
		Policy:  policy.Fixed(&set),
	}, store, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return instance
}

func allowPrefix(set *policy.Set, bucket, prefix string, ops ...authority.Operation) {
	set.Add("runtime", bucket, policy.Selector{Kind: policy.KindObject, Prefix: prefix}, policy.Allow, ops...)
}

// Authority alone cannot say "everything except this key", which is what a
// policy is for.
func TestAPolicyNarrowsAFullAuthorityToOnePrefix(t *testing.T) {
	instance := openPolicy(t, func(set *policy.Set) {
		allowPrefix(set, "public", "public/", authority.ObjectRead, authority.ObjectWrite)
	})
	ctx := context.Background()

	if _, err := instance.GetObject(ctx, "public", "readme"); !denied(t, err) {
		t.Fatalf("GetObject on an uncovered key: %v", err)
	}
	if _, err := instance.PutObject(ctx, "public", "readme", []byte("x"), runtime.PutOptions{}); !denied(t, err) {
		t.Fatalf("PutObject on an uncovered key: %v", err)
	}
	// The covered key is writable, so this is not refusing everything.
	if _, err := instance.PutObject(ctx, "public", "public/page", []byte("x"), runtime.PutOptions{}); err != nil {
		t.Fatalf("PutObject on a covered key: %v", err)
	}
	if _, err := instance.GetObject(ctx, "public", "public/page"); err != nil {
		t.Fatalf("GetObject on a covered key: %v", err)
	}
	// A different bucket is a different collection.
	if _, err := instance.GetObject(ctx, "private", "public/page"); !denied(t, err) {
		t.Fatalf("GetObject in an uncovered collection: %v", err)
	}
}

// The object side of the prefix rule: `pub` covers `publicish` too, because
// that is what an S3 prefix has always meant. A workspace subtree would not.
func TestAnObjectPrefixKeepsS3PrefixSemantics(t *testing.T) {
	instance := openPolicy(t, func(set *policy.Set) {
		allowPrefix(set, "public", "pub", authority.ObjectRead)
	})
	ctx := context.Background()

	if _, err := instance.GetObject(ctx, "public", "public/page"); err != nil {
		t.Fatalf("covered by the prefix: %v", err)
	}
	if _, err := instance.GetObject(ctx, "public", "publicish"); err != nil {
		t.Fatalf("covered by the prefix, as an S3 prefix is: %v", err)
	}
	if _, err := instance.GetObject(ctx, "public", "private/page"); !denied(t, err) {
		t.Fatalf("not covered by the prefix: %v", err)
	}
}

// A list is authorized on the caller's own prefix, which is sound only because
// the results are inside it. So a grant on `public/` permits a list scoped to it
// and refuses the same list unscoped; filtering a broad list down to a grant is
// the open half of enumeration.
func TestAListIsScopedToTheCallersPrefix(t *testing.T) {
	instance := openPolicy(t, func(set *policy.Set) {
		allowPrefix(set, "public", "public/", authority.ObjectList)
	})
	ctx := context.Background()

	if _, err := instance.ListObjects(ctx, "public", runtime.ListOptions{Prefix: "public/"}); err != nil {
		t.Fatalf("list within the granted prefix: %v", err)
	}
	if _, err := instance.ListObjects(ctx, "public", runtime.ListOptions{}); !denied(t, err) {
		t.Fatal("an unscoped list must be refused while a scoped one is allowed")
	}
}

// A copy reads its source and writes its destination, so both ends are
// authorized. Checking only the destination let a write-only caller copy content
// out: the store read the source and the guard saw a permitted write.
func TestACopyIsAuthorizedOnBothEnds(t *testing.T) {
	writeOnly := authority.None().With(authority.ObjectWrite, authority.ObjectList, authority.BucketCreate)
	instance := openWith(t, writeOnly)
	ctx := context.Background()
	if _, err := instance.PutObject(ctx, "bucket", "secret", []byte("sensitive"), runtime.PutOptions{}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := instance.CopyObject(ctx, "bucket", "secret", "bucket", "copy"); !denied(t, err) {
		t.Fatal("a write-only environment copied object content out")
	}
	// The destination is not left holding the copy, so the refusal is not cosmetic.
	if _, err := instance.HeadObject(ctx, "bucket", "copy"); err == nil {
		t.Fatal("the refused copy still published its destination")
	}
}

// A grant to write one prefix must not become a way to read another by copying.
func TestACopyCannotCarryContentPastAPolicy(t *testing.T) {
	instance := openPolicy(t, func(set *policy.Set) {
		allowPrefix(set, "private", "private/", authority.ObjectWrite)
	})
	ctx := context.Background()

	if _, err := instance.CopyObject(ctx, "public", "readme", "private", "private/stolen"); !denied(t, err) {
		t.Fatal("a policy-scoped write copied from an unreadable source")
	}
}

// Multipart continues by upload ID, so the resource is resolved, not trusted.
func TestAMultipartContinuationIsAuthorizedOnItsTarget(t *testing.T) {
	instance := openPolicy(t, func(set *policy.Set) {
		allowPrefix(set, "public", "public/", authority.ObjectRead, authority.ObjectWrite)
	})
	ctx := context.Background()

	upload, err := instance.CreateMultipartUpload(ctx, "public", "public/big", storage.MultipartOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := instance.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("part")); err != nil {
		t.Fatalf("upload part to a granted target: %v", err)
	}

	// An upload of a denied target never starts, so there is nothing to continue.
	if _, err := instance.CreateMultipartUpload(ctx, "public", "private/big", storage.MultipartOptions{}); !denied(t, err) {
		t.Fatal("multipart initiation on a denied target was allowed")
	}
}

// A policy wider than its environment is refused rather than clipped, and it
// refuses everything: clipping it silently would leave the author believing a
// permission is in force when it is not.
func TestAPolicyWiderThanTheEnvironmentRefusesEverything(t *testing.T) {
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(context.Background(), "public"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	readOnly := authority.None().With(authority.ObjectRead)
	var set policy.Set
	allowPrefix(&set, "public", "public/", authority.ObjectRead, authority.ObjectWrite)

	instance, err := runtime.OpenWithStore(runtime.Options{
		Backend:   runtime.BackendMemory,
		Authority: &readOnly,
		Policy:    policy.Fixed(&set),
	}, store, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer instance.Close()

	if _, err := instance.GetObject(context.Background(), "public", "public/page"); err == nil {
		t.Fatal("a policy that widens the environment was accepted")
	} else if !errors.Is(err, policy.ErrWidening) {
		t.Fatalf("want ErrWidening, got %v", err)
	}
}

// Unknown, so a caller can tell "you may not" from "I no longer know".
func TestAnExpiredPolicyIsUnknownRatherThanDenied(t *testing.T) {
	instance := openPolicy(t, func(set *policy.Set) {
		set.Expires = time.Now().Add(-time.Minute)
		set.Now = func() time.Time { return time.Now() }
		allowPrefix(set, "public", "public/", authority.ObjectRead)
	})

	_, err := instance.GetObject(context.Background(), "public", "public/page")
	if denied(t, err) {
		t.Fatal("an expired policy answered as a permission decision")
	}
	if !errors.Is(err, policy.ErrStalePolicy) {
		t.Fatalf("want ErrStalePolicy, got %v", err)
	}
}

// What makes a deny usable as an exception inside a broad grant.
func TestAnExplicitDenyBeatsABroadAllow(t *testing.T) {
	instance := openPolicy(t, func(set *policy.Set) {
		allowPrefix(set, "public", "public/", authority.ObjectRead, authority.ObjectWrite)
		allowPrefix(set, "public", "public/secret", authority.ObjectRead, authority.ObjectWrite)
		set.Add("runtime", "public", policy.Selector{Kind: policy.KindObject, Prefix: "public/secret"},
			policy.Deny, authority.ObjectRead, authority.ObjectWrite)
	})
	ctx := context.Background()

	if _, err := instance.GetObject(ctx, "public", "public/page"); err != nil {
		t.Fatalf("the broad grant still applies: %v", err)
	}
	if _, err := instance.GetObject(ctx, "public", "public/secret"); !denied(t, err) {
		t.Fatal("the explicit deny did not beat the broad allow")
	}
}

// A batch delete authorizes each key, and a refusal leaves the batch unapplied:
// deleting the permitted keys and refusing the rest would be a partial answer to
// a question the caller asked as one.
func TestABatchDeleteIsAuthorizedPerKey(t *testing.T) {
	instance := openPolicy(t, func(set *policy.Set) {
		allowPrefix(set, "public", "public/", authority.ObjectDelete)
	})
	ctx := context.Background()

	if _, err := instance.DeleteObjects(ctx, "public", []string{"public/a", "private/b"}); !denied(t, err) {
		t.Fatal("a batch containing a denied key was allowed")
	}
	if _, err := instance.HeadObject(ctx, "public", "public/a"); err == nil {
		t.Fatal("the permitted key was deleted despite the batch being refused")
	}
}

// The gate. An object operation reaches the policy through checkResource or
// checkUpload; one that used check would consult no policy at all, which is
// bypass-by-omission rather than a gap somebody decided on. The recorded
// exception is the recovery-hold fallback for a hold this instance cannot
// attribute to any object.
var resourceLessObjectChecks = map[string]string{
	"object_recovery.go": "checkRecoveryAuthority's empty-refs branch: a hold named by ID alone has no resource to match, and an unattributable hold is not evidence the caller may act on it",
}

func TestObjectChecksNameAResource(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read runtime package: %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, site := range resourceLessObjectCheckSites(fset, name, file) {
			if reason, recorded := resourceLessObjectChecks[name]; recorded {
				t.Logf("%s checks %s through check: %s", name, site.op, reason)
				continue
			}
			t.Errorf("%s:%d checks %s through check, which consults no policy; use checkResource or checkUpload",
				name, site.line, site.op)
		}
	}
}

type objectCheckSite struct {
	op   string
	line int
}

// resourceLessObjectCheckSites returns every i.check(authority.Object*) call in one
// file: the shape naming an operation but no resource. Matching the call's shape
// rather than any mention of an operation is deliberate — a slice or a log line
// naming one proves nothing about enforcement.
func resourceLessObjectCheckSites(fset *token.FileSet, name string, file *ast.File) []objectCheckSite {
	var sites []objectCheckSite
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "check" {
			return true
		}
		lit, ok := call.Args[0].(*ast.SelectorExpr)
		if !ok || !strings.HasPrefix(lit.Sel.Name, "Object") {
			return true
		}
		if pkg, ok := lit.X.(*ast.Ident); !ok || pkg.Name != "authority" {
			return true
		}
		sites = append(sites, objectCheckSite{op: lit.Sel.Name, line: fset.Position(call.Pos()).Line})
		return true
	})
	return sites
}
