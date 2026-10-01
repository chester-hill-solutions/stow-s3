package runtime_test

import (
	"context"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// A listing used to consult the policy exactly once, on the prefix the caller asked
// for, and then return every key the store listed.
//
// That could not honour a per-key deny at all, and not because of a bug in the
// matcher: a listing asks about a *prefix*, and `public/secret` is not
// `public/secret/`, so an exact selector naming one key never matches the prefix a
// list is decided on. A policy denying enumeration of a key was written, validated,
// persisted, and did nothing — while still letting a caller aim a listing straight at
// the denied key and be told yes.
//
// These cases pin the two halves of the repair: the decision is taken per key, and the
// count describes what the caller was given rather than what exists.

const enumBucket = "probe-bucket"

func enumInstance(t *testing.T, build func(*policy.Set), keys map[string]string) *runtime.Instance {
	t.Helper()
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(context.Background(), enumBucket); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	for key, body := range keys {
		if _, err := store.PutObject(context.Background(), enumBucket, key, strings.NewReader(body), storage.PutOptions{}); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	var set policy.Set
	set.Revision = "enum"
	build(&set)
	instance, err := runtime.OpenWithStore(
		runtime.Options{Backend: runtime.BackendMemory, Policy: policy.Fixed(&set)},
		store, func() (storage.Store, error) { return storage.NewMemoryStore(), nil })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	return instance
}

func listed(page runtime.ObjectPage) []string {
	out := make([]string, 0, len(page.Objects))
	for _, o := range page.Objects {
		out = append(out, o.Key)
	}
	return out
}

func TestAListingOmitsAKeyThePolicyDeniesEnumerating(t *testing.T) {
	instance := enumInstance(t, func(set *policy.Set) {
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Prefix: "public/"},
			policy.Allow, authority.ObjectRead, authority.ObjectList)
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Exact: "public/secret"},
			policy.Deny, authority.ObjectList)
	}, map[string]string{
		"public/readme": "open",
		"public/secret": "classified",
		"public/shared": "also open",
		"private/keys":  "nope",
	})

	page, err := instance.ListObjects(context.Background(), enumBucket, runtime.ListOptions{Prefix: "public/"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, key := range listed(page) {
		if key == "public/secret" {
			t.Fatal("a listing disclosed a key the policy denies enumerating")
		}
	}
	if len(page.Objects) != 2 {
		t.Errorf("listed %v, want the two permitted keys under the prefix", listed(page))
	}
}

func TestTheCountDescribesWhatWasReturnedNotWhatExists(t *testing.T) {
	// A count the caller cannot account for is a disclosure on its own: it says how
	// much exists that this caller was not shown.
	instance := enumInstance(t, func(set *policy.Set) {
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Prefix: "public/"},
			policy.Allow, authority.ObjectRead, authority.ObjectList)
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Exact: "public/secret"},
			policy.Deny, authority.ObjectList)
	}, map[string]string{
		"public/readme": "open",
		"public/secret": "classified",
	})

	page, err := instance.ListObjects(context.Background(), enumBucket, runtime.ListOptions{Prefix: "public/"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.KeyCount != len(page.Objects) {
		t.Errorf("KeyCount = %d but %d keys were returned; the count describes the store, not the caller",
			page.KeyCount, len(page.Objects))
	}
}

func TestAListingAimedAtADeniedSubtreeYieldsNothing(t *testing.T) {
	// The sharp half. Naming the denied prefix used to be permitted, because the allow
	// on `public/` matched the prefix asked for and an exact deny on one key was
	// structurally unable to.
	//
	// The deny here is a prefix, not an exact key, because that is how a subtree is
	// written in this model: an exact selector denies one object and has never claimed
	// to deny everything beneath it. With an exact deny the key inside the subtree is
	// permitted, which is the matcher's contract rather than a leak.
	instance := enumInstance(t, func(set *policy.Set) {
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Prefix: "public/"},
			policy.Allow, authority.ObjectRead, authority.ObjectList)
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Prefix: "public/secret"},
			policy.Deny, authority.ObjectList)
	}, map[string]string{"public/secret/inner": "classified"})

	page, err := instance.ListObjects(context.Background(), enumBucket, runtime.ListOptions{Prefix: "public/secret/"})
	if err == nil && len(page.Objects) != 0 {
		t.Errorf("a listing scoped to a denied subtree returned %v", listed(page))
	}
	if err == nil && page.KeyCount != 0 {
		t.Errorf("KeyCount = %d for a denied subtree, want 0", page.KeyCount)
	}
}

func TestAnExactDenyCoversOneObjectAndNotItsSubtree(t *testing.T) {
	// The other half of the same rule, stated so the distinction cannot be lost: this
	// is what an exact selector means, and a test that assumed otherwise is a test
	// asserting a widening.
	instance := enumInstance(t, func(set *policy.Set) {
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Prefix: "public/"},
			policy.Allow, authority.ObjectRead, authority.ObjectList)
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Exact: "public/secret"},
			policy.Deny, authority.ObjectList)
	}, map[string]string{
		"public/secret":       "classified",
		"public/secret/inner": "classified, but a different key",
	})

	page, err := instance.ListObjects(context.Background(), enumBucket, runtime.ListOptions{Prefix: "public/"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	keys := listed(page)
	for _, key := range keys {
		if key == "public/secret" {
			t.Error("the exactly-denied key was disclosed")
		}
	}
	if !contains(keys, "public/secret/inner") {
		t.Errorf("listed %v; an exact deny on one object has never claimed to deny its subtree", keys)
	}
}

func contains(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}

func TestTheCountCoversRolledUpPrefixesToo(t *testing.T) {
	// The half a non-delimiter test cannot see. S3 counts returned *entries*, so a
	// delimiter page with no keys and two rolled-up prefixes reports 2, not 0. The
	// first version of the filtering used the key count alone and the shared corpus
	// failed it, which is the corpus earning its place as the independent second
	// opinion; this case keeps the arithmetic pinned on both shapes.
	instance := enumInstance(t, func(set *policy.Set) {
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Prefix: "public/"},
			policy.Allow, authority.ObjectRead, authority.ObjectList)
	}, map[string]string{
		"root.txt":   "root",
		"public/a/1": "a",
		"public/b/1": "b",
	})

	page, err := instance.ListObjects(context.Background(), enumBucket,
		runtime.ListOptions{Prefix: "public/", Delimiter: "/"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Objects) != 0 {
		t.Fatalf("listed keys %v, want none: every key is under a rolled-up prefix", listed(page))
	}
	if len(page.CommonPrefixes) != 2 {
		t.Fatalf("CommonPrefixes = %v, want two", page.CommonPrefixes)
	}
	if page.KeyCount != 2 {
		t.Errorf("KeyCount = %d, want 2: the count is returned entries, keys plus prefixes", page.KeyCount)
	}
}

func TestACommonPrefixIsAlsoFiltered(t *testing.T) {
	// With a delimiter the store answers with prefixes rather than keys, and a
	// filtered-keys-only change would have disclosed a denied subtree as a name.
	instance := enumInstance(t, func(set *policy.Set) {
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Prefix: "public/"},
			policy.Allow, authority.ObjectRead, authority.ObjectList)
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Prefix: "public/secret"},
			policy.Deny, authority.ObjectList)
	}, map[string]string{
		"public/readme":      "open",
		"public/secret/file": "classified",
	})

	// Scoped to the allowed prefix, so the request itself is permitted and what is
	// being tested is the per-prefix filter rather than the up-front check.
	page, err := instance.ListObjects(context.Background(), enumBucket,
		runtime.ListOptions{Prefix: "public/", Delimiter: "/"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, prefix := range page.CommonPrefixes {
		if strings.HasPrefix(prefix, "public/secret") {
			t.Errorf("CommonPrefixes disclosed a denied subtree as %q", prefix)
		}
	}
}

func TestNoPolicyStillListsEverything(t *testing.T) {
	// The default must not have changed, which is what makes this a fix rather than a
	// tightening.
	store := storage.NewMemoryStore()
	if err := store.CreateBucket(context.Background(), enumBucket); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	for _, key := range []string{"a", "b", "c"} {
		if _, err := store.PutObject(context.Background(), enumBucket, key, strings.NewReader("x"), storage.PutOptions{}); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	instance, err := runtime.OpenWithStore(runtime.Options{Backend: runtime.BackendMemory},
		store, func() (storage.Store, error) { return storage.NewMemoryStore(), nil })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer instance.Close()

	page, err := instance.ListObjects(context.Background(), enumBucket, runtime.ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Objects) != 3 || page.KeyCount != 3 {
		t.Errorf("without a policy listed %d keys with KeyCount %d, want 3 and 3", len(page.Objects), page.KeyCount)
	}
}

func TestAPolicyThatWithholdsListRefusesTheListing(t *testing.T) {
	// Withheld entirely rather than answered with an empty page, because a caller who
	// asked to enumerate and was told "nothing" has been told something false.
	instance := enumInstance(t, func(set *policy.Set) {
		set.Add(policy.LocalNamespace, enumBucket,
			policy.Selector{Kind: policy.KindObject, Prefix: "public/"},
			policy.Allow, authority.ObjectRead)
	}, map[string]string{"public/readme": "open"})

	if _, err := instance.ListObjects(context.Background(), enumBucket, runtime.ListOptions{Prefix: "public/"}); err == nil {
		t.Error("a policy withholding object.list permitted a listing")
	}
}
