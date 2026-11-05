package s3api_test

import (
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestListObjectsV1WithoutDelimiterOmitsNextMarker(t *testing.T) {
	for name, newStore := range wireStores() {
		t.Run(name, func(t *testing.T) {
			ts := newStoreServer(t, newStore(t))
			seedV1Bucket(t, ts, "flat-cursor")
			listing, status, body := listV1(t, ts, "flat-cursor", v1Request{maxKeys: 1})
			if status != http.StatusOK || !listing.IsTruncated || len(listing.Contents) != 1 {
				t.Fatalf("expected a truncated page: %d %s", status, body)
			}
			if strings.Contains(body, "<NextMarker>") {
				t.Errorf("NextMarker requires a delimiter; flat clients must page by the last Key: %s", body)
			}
		})
	}
}

func v1NextPageMarker(t *testing.T, listing v1Listing, delimiter string) string {
	t.Helper()
	if delimiter != "" {
		if listing.NextMarker == "" {
			t.Fatal("truncated delimiter page has no NextMarker")
		}
		return listing.NextMarker
	}
	if listing.NextMarker != "" {
		t.Fatal("flat listing must not emit NextMarker")
	}
	if len(listing.Contents) == 0 {
		t.Fatal("truncated flat page has no last Key")
	}
	return listing.Contents[len(listing.Contents)-1].Key
}

func TestListObjectsV1DoesNotAnswerUnsupportedSelectors(t *testing.T) {
	ts := newStoreServer(t, storage.NewMemoryStore())
	createBucketOverWire(t, ts, "selectors")
	for _, query := range []string{"cors", "ownershipControls", "publicAccessBlock", "policyStatus", "list-type=3", "list-type="} {
		t.Run(query, func(t *testing.T) {
			response, err := http.Get(ts.URL + "/selectors?" + query)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus, wantCode := http.StatusNotImplemented, "NotImplemented"
			if strings.HasPrefix(query, "list-type=") {
				wantStatus, wantCode = http.StatusBadRequest, "InvalidArgument"
			}
			if response.StatusCode != wantStatus || !strings.Contains(string(body), "<Code>"+wantCode+"</Code>") {
				t.Errorf("unsupported selector answered as %d %s", response.StatusCode, body)
			}
		})
	}
}
