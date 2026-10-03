package runtime_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
)

func TestScopedListingCursorDoesNotDiscloseDeniedKeys(t *testing.T) {
	for _, delimiter := range []string{"", "/"} {
		t.Run("delimiter="+delimiter, func(t *testing.T) {
			keys := map[string]string{}
			for _, key := range []string{"a-hidden", "b-open", "c-hidden", "d-open", "z-hidden"} {
				if delimiter != "" {
					key += "/file"
				}
				keys["public/"+key] = "body"
			}
			instance := enumInstance(t, denyHiddenListing, keys)
			options := runtime.ListOptions{Prefix: "public/", Delimiter: delimiter, Limit: 1}
			var seen []string
			for pageNumber := 0; pageNumber < 3; pageNumber++ {
				page, err := instance.ListObjects(context.Background(), enumBucket, options)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(page.Cursor, "hidden") || strings.Contains(page.NextCursor, "hidden") {
					t.Fatalf("denied key in cursor: %+v", page)
				}
				seen = append(seen, listed(page)...)
				seen = append(seen, page.CommonPrefixes...)
				if page.KeyCount != 1 {
					t.Fatalf("page did not contain one visible entry: %+v", page)
				}
				if !page.Truncated {
					if len(seen) != 2 || page.NextCursor != "" {
						t.Fatalf("incomplete or stale continuation: %+v, seen=%v", page, seen)
					}
					return
				}
				options.Cursor = page.NextCursor
			}
			t.Fatal("scoped listing did not terminate")
		})
	}
}

func denyHiddenListing(set *policy.Set) {
	set.Add(policy.LocalNamespace, enumBucket, policy.Selector{Kind: policy.KindObject, Prefix: "public/"}, policy.Allow, authority.ObjectList)
	for _, key := range []string{"a-hidden", "c-hidden", "z-hidden"} {
		set.Add(policy.LocalNamespace, enumBucket, policy.Selector{Kind: policy.KindObject, Prefix: "public/" + key}, policy.Deny, authority.ObjectList)
	}
}

func TestScopedListingWithOnlyDeniedKeysHasNoContinuation(t *testing.T) {
	instance := enumInstance(t, denyHiddenListing, map[string]string{"public/a-hidden": "a", "public/c-hidden": "c"})
	page, err := instance.ListObjects(context.Background(), enumBucket, runtime.ListOptions{Prefix: "public/", Limit: 1})
	if err != nil || page.Truncated || page.NextCursor != "" || page.KeyCount != 0 {
		t.Fatalf("denied-only listing: %+v, error=%v", page, err)
	}
}

func TestScopedListingCrossesDeniedBackendPages(t *testing.T) {
	keys := map[string]string{"public/b-open": "b", "public/d-open": "d"}
	for n := 0; n < 1001; n++ {
		keys[fmt.Sprintf("public/a-hidden/%04d", n)] = "hidden"
	}
	instance := enumInstance(t, denyHiddenListing, keys)
	opts := runtime.ListOptions{Prefix: "public/", Limit: 1}
	first, err := instance.ListObjects(context.Background(), enumBucket, opts)
	if err != nil || len(first.Objects) != 1 || first.Objects[0].Key != "public/b-open" || !first.Truncated {
		t.Fatalf("first visible page: %+v, %v", first, err)
	}
	opts.Cursor = first.NextCursor
	last, err := instance.ListObjects(context.Background(), enumBucket, opts)
	if err != nil || len(last.Objects) != 1 || last.Objects[0].Key != "public/d-open" || last.Truncated {
		t.Fatalf("last visible page: %+v, %v", last, err)
	}
}
