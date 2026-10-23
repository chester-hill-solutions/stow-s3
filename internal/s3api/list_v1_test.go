package s3api_test

// ListObjects (V1) wire tests.
//
// V1 is what a client that never learned the list-type marker sends, and the
// marker is the part of it with no V2 equivalent: V1 pages by naming a row of
// the listing, which may be a common prefix. These tests assert the rows a
// client sees, against every store the S3 surface is served from.

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// v1Listing is the V1 response as a client decodes it. The test parses this
// rather than the server's own type, so a field renamed in one place and not the
// other fails here rather than passing on both sides.
type v1Listing struct {
	Name        string `xml:"Name"`
	Prefix      string `xml:"Prefix"`
	Marker      string `xml:"Marker"`
	NextMarker  string `xml:"NextMarker"`
	MaxKeys     int    `xml:"MaxKeys"`
	Delimiter   string `xml:"Delimiter"`
	IsTruncated bool   `xml:"IsTruncated"`
	Contents    []struct {
		Key          string `xml:"Key"`
		LastModified string `xml:"LastModified"`
		ETag         string `xml:"ETag"`
		Size         int64  `xml:"Size"`
		Owner        struct {
			ID          string `xml:"ID"`
			DisplayName string `xml:"DisplayName"`
		} `xml:"Owner"`
		StorageClass string `xml:"StorageClass"`
	} `xml:"Contents"`
	CommonPrefixes []struct {
		Prefix string `xml:"Prefix"`
	} `xml:"CommonPrefixes"`
	EncodingType string `xml:"EncodingType"`
}

// v1Request is the listing a test asks for, encoded the way a client encodes it
// so a test cannot pass by spelling a query the server would never receive.
type v1Request struct {
	prefix       string
	delimiter    string
	marker       string
	encodingType string
	maxKeys      int
}

func (r v1Request) query() string {
	values := url.Values{}
	if r.prefix != "" {
		values.Set("prefix", r.prefix)
	}
	if r.delimiter != "" {
		values.Set("delimiter", r.delimiter)
	}
	if r.marker != "" {
		values.Set("marker", r.marker)
	}
	if r.encodingType != "" {
		values.Set("encoding-type", r.encodingType)
	}
	if r.maxKeys != 0 {
		values.Set("max-keys", strconv.Itoa(r.maxKeys))
	}
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}

// v1Rows flattens a listing into the rows it reported, sorted. Rows are keys and
// common prefixes together, because a marker names a row and a client that pages
// cannot tell the two kinds apart.
func v1Rows(listing v1Listing) []string {
	rows := make([]string, 0, len(listing.Contents)+len(listing.CommonPrefixes))
	for _, entry := range listing.Contents {
		rows = append(rows, entry.Key)
	}
	for _, prefix := range listing.CommonPrefixes {
		rows = append(rows, prefix.Prefix)
	}
	slices.Sort(rows)
	return rows
}

func listV1(t *testing.T, ts *httptest.Server, bucket string, request v1Request) (v1Listing, int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/"+bucket+request.query(), nil)
	if err != nil {
		t.Fatalf("build list %q: %v", request.query(), err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list %q: %v", request.query(), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return v1Listing{}, resp.StatusCode, string(body)
	}
	var listing v1Listing
	if err := xml.Unmarshal(body, &listing); err != nil {
		t.Fatalf("parse listing %q: %v", body, err)
	}
	return listing, resp.StatusCode, string(body)
}

// v1Keys is the fixture MinIO's own ListObjects matrix uses. The expected rows
// below are the ones an S3-compatible server is measured against, not rows
// invented here.
var v1Keys = []string{
	"Asia-maps.png",
	"Asia/India/India-summer-photos-1",
	"Asia/India/Karnataka/Bangalore/Koramangala/pics",
	"newPrefix0",
	"newPrefix1",
	"newzen/zen/recurse/again/again/again/pics",
	"obj0",
	"obj1",
	"obj2",
}

// v1DelimitedRows is v1Keys as rows under a "/" delimiter: the three Asia keys
// collapse into one row and the newzen key into another.
var v1DelimitedRows = []string{
	"Asia-maps.png",
	"Asia/",
	"newPrefix0",
	"newPrefix1",
	"newzen/",
	"obj0",
	"obj1",
	"obj2",
}

func seedV1Bucket(t *testing.T, ts *httptest.Server, bucket string) {
	t.Helper()
	createBucketOverWire(t, ts, bucket)
	for _, key := range v1Keys {
		putOverWire(t, ts, objectRef{bucket: bucket, key: key}, "content of "+key)
	}
}

func TestListObjectsV1IsTheClassicListing(t *testing.T) {
	for name, newStore := range wireStores() {
		t.Run(name, func(t *testing.T) {
			ts := newStoreServer(t, newStore(t))
			seedV1Bucket(t, ts, "classic")

			listing, status, body := listV1(t, ts, "classic", v1Request{})
			if status != http.StatusOK {
				t.Fatalf("list status = %d: %s", status, body)
			}
			if got := v1Rows(listing); !slices.Equal(got, v1Keys) {
				t.Errorf("rows = %v, want %v", got, v1Keys)
			}
			if listing.Name != "classic" {
				t.Errorf("Name = %q, want the bucket", listing.Name)
			}
			if listing.Marker != "" || listing.NextMarker != "" {
				t.Errorf("Marker = %q, NextMarker = %q, want both empty on an unpaged first page", listing.Marker, listing.NextMarker)
			}
			if listing.IsTruncated {
				t.Error("a listing of nine keys under the default page is truncated")
			}
			if listing.MaxKeys != 1000 {
				t.Errorf("MaxKeys = %d, want the default page", listing.MaxKeys)
			}
			// V1 always sends an owner per row, unlike V2. A client that reads it
			// must not get a zero value.
			for _, entry := range listing.Contents {
				if entry.Owner.ID == "" || entry.Owner.DisplayName == "" {
					t.Errorf("row %q carries no owner: %+v", entry.Key, entry.Owner)
				}
				if entry.ETag == "" || entry.StorageClass != "STANDARD" {
					t.Errorf("row %q has ETag %q and class %q", entry.Key, entry.ETag, entry.StorageClass)
				}
			}
		})
	}
}

func TestListObjectsV1GroupsKeysUnderTheDelimiter(t *testing.T) {
	for name, newStore := range wireStores() {
		t.Run(name, func(t *testing.T) {
			ts := newStoreServer(t, newStore(t))
			seedV1Bucket(t, ts, "grouped")

			listing, status, body := listV1(t, ts, "grouped", v1Request{delimiter: "/"})
			if status != http.StatusOK {
				t.Fatalf("list status = %d: %s", status, body)
			}
			if got := v1Rows(listing); !slices.Equal(got, v1DelimitedRows) {
				t.Errorf("rows = %v, want %v", got, v1DelimitedRows)
			}
			if listing.Delimiter != "/" {
				t.Errorf("Delimiter = %q, want the delimiter that was sent", listing.Delimiter)
			}
		})
	}
}

// TestListObjectsV1MarkerNamesARow pins the rule with no V2 equivalent: a marker
// inside a delimiter group skips the whole group. Re-emitting the group's
// remaining keys on every page is the duplicate-row loop a sync or mount client
// falls into, so each expected row set here is a page a client could page past
// without repeating itself.
func TestListObjectsV1MarkerNamesARow(t *testing.T) {
	cases := []struct {
		name    string
		request v1Request
		want    []string
	}{
		{
			name:    "a marker inside a group skips the group",
			request: v1Request{delimiter: "/", marker: "Asia/India/India-summer-photos-1"},
			want:    []string{"newPrefix0", "newPrefix1", "newzen/", "obj0", "obj1", "obj2"},
		},
		{
			name:    "a marker deep inside a group skips the group",
			request: v1Request{delimiter: "/", marker: "Asia/India/Karnataka/Bangalore/Koramangala/pics"},
			want:    []string{"newPrefix0", "newPrefix1", "newzen/", "obj0", "obj1", "obj2"},
		},
		{
			name:    "a marker that is itself a group does not re-emit it",
			request: v1Request{delimiter: "/", marker: "newzen/"},
			want:    []string{"obj0", "obj1", "obj2"},
		},
		{
			name:    "without a delimiter a marker is the key it names",
			request: v1Request{marker: "Asia/India/India-summer-photos-1"},
			want: []string{
				"Asia/India/Karnataka/Bangalore/Koramangala/pics",
				"newPrefix0", "newPrefix1",
				"newzen/zen/recurse/again/again/again/pics",
				"obj0", "obj1", "obj2",
			},
		},
		{
			name:    "a marker past every row lists nothing",
			request: v1Request{delimiter: "/", marker: "zen"},
			want:    nil,
		},
		{
			name:    "a marker before the first row lists everything",
			request: v1Request{delimiter: "/", marker: "Abc"},
			want:    v1DelimitedRows,
		},
	}

	for name, newStore := range wireStores() {
		t.Run(name, func(t *testing.T) {
			for _, testCase := range cases {
				t.Run(testCase.name, func(t *testing.T) {
					ts := newStoreServer(t, newStore(t))
					seedV1Bucket(t, ts, "markers")

					listing, status, body := listV1(t, ts, "markers", testCase.request)
					if status != http.StatusOK {
						t.Fatalf("list status = %d: %s", status, body)
					}
					got := v1Rows(listing)
					if len(got) == 0 {
						got = nil
					}
					if !slices.Equal(got, testCase.want) {
						t.Errorf("rows = %v, want %v", got, testCase.want)
					}
					if listing.Marker != testCase.request.marker {
						t.Errorf("Marker = %q, want the marker that was sent (%q)", listing.Marker, testCase.request.marker)
					}
				})
			}
		})
	}
}

// TestListObjectsV1PagingCoversEveryRowOnce pages a listing the way a client
// does, by handing each page's NextMarker back as the next marker, and asserts
// the pages cover the listing exactly. A page that repeats a row would show up
// here as a duplicate, and a page that never advances as the page-count guard.
func TestListObjectsV1PagingCoversEveryRowOnce(t *testing.T) {
	cases := []struct {
		name    string
		request v1Request
		want    []string
	}{
		{name: "with a delimiter", request: v1Request{delimiter: "/", maxKeys: 1}, want: v1DelimitedRows},
		{name: "without a delimiter", request: v1Request{maxKeys: 2}, want: v1Keys},
	}

	for name, newStore := range wireStores() {
		t.Run(name, func(t *testing.T) {
			for _, testCase := range cases {
				t.Run(testCase.name, func(t *testing.T) {
					ts := newStoreServer(t, newStore(t))
					seedV1Bucket(t, ts, "paging")

					marker := ""
					var rows []string
					for page := 1; ; page++ {
						if page > 20 {
							t.Fatalf("paging did not finish in 20 pages; the last marker was %q, which is the shape of a page that never advances", marker)
						}
						request := testCase.request
						request.marker = marker
						listing, status, body := listV1(t, ts, "paging", request)
						if status != http.StatusOK {
							t.Fatalf("page %d status = %d: %s", page, status, body)
						}
						rows = append(rows, v1Rows(listing)...)
						if !listing.IsTruncated {
							if listing.NextMarker != "" {
								t.Errorf("the final page reported NextMarker %q", listing.NextMarker)
							}
							break
						}
						if listing.NextMarker == "" {
							t.Fatalf("page %d is truncated with no NextMarker, so a client has nothing to page with", page)
						}
						marker = listing.NextMarker
					}
					slices.Sort(rows)
					if !slices.Equal(rows, testCase.want) {
						t.Errorf("the pages returned %v, want %v", rows, testCase.want)
					}
				})
			}
		})
	}
}

func TestListObjectsV1PrefixAndURLEncoding(t *testing.T) {
	ts := newStoreServer(t, storage.NewMemoryStore())
	createBucketOverWire(t, ts, "encoded")
	for _, key := range []string{"a/one.txt", "a/two.txt", "b/one.txt", "spaced key.txt"} {
		putOverWire(t, ts, objectRef{bucket: "encoded", key: key}, "content of "+key)
	}

	listing, status, body := listV1(t, ts, "encoded", v1Request{prefix: "a/", delimiter: "/"})
	if status != http.StatusOK {
		t.Fatalf("prefixed list status = %d: %s", status, body)
	}
	if got := v1Rows(listing); !slices.Equal(got, []string{"a/one.txt", "a/two.txt"}) {
		t.Errorf("rows = %v, want the two keys under the prefix", got)
	}

	listing, status, body = listV1(t, ts, "encoded", v1Request{encodingType: "url"})
	if status != http.StatusOK {
		t.Fatalf("encoded list status = %d: %s", status, body)
	}
	if listing.EncodingType != "url" {
		t.Errorf("EncodingType = %q, want url", listing.EncodingType)
	}
	if !strings.Contains(body, "spaced%20key.txt") {
		t.Errorf("the encoded listing does not carry the encoded key: %s", body)
	}
	if strings.Contains(body, "<Key>spaced key.txt</Key>") {
		t.Errorf("the encoded listing carries the raw key: %s", body)
	}
}

// TestListObjectsV1MaxKeysFallsBackToTheDefaultPage pins the product's own
// convention rather than AWS's: a zero or unparseable max-keys means the default
// page, which is what the V2 handler and the runtime layer already do. Diverging
// between the two listing versions would make the same parameter mean two
// things.
func TestListObjectsV1MaxKeysFallsBackToTheDefaultPage(t *testing.T) {
	for _, maxKeys := range []string{"0", "abc", "-1"} {
		t.Run("max-keys="+maxKeys, func(t *testing.T) {
			ts := newStoreServer(t, storage.NewMemoryStore())
			seedV1Bucket(t, ts, "defaulted")

			request, err := http.NewRequest(http.MethodGet, ts.URL+"/defaulted?max-keys="+maxKeys, nil)
			if err != nil {
				t.Fatalf("build list: %v", err)
			}
			resp, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("list status = %d: %s", resp.StatusCode, body)
			}
			var listing v1Listing
			if err := xml.Unmarshal(body, &listing); err != nil {
				t.Fatalf("parse listing %q: %v", body, err)
			}
			if listing.MaxKeys != 1000 {
				t.Errorf("MaxKeys = %d, want the default page", listing.MaxKeys)
			}
			if got := v1Rows(listing); !slices.Equal(got, v1Keys) {
				t.Errorf("rows = %v, want the whole listing", got)
			}
		})
	}
}

func TestListObjectsV1MissingBucketIsNoSuchBucket(t *testing.T) {
	ts := newStoreServer(t, storage.NewMemoryStore())

	_, status, body := listV1(t, ts, "absent", v1Request{})
	if status != http.StatusNotFound {
		t.Errorf("list status = %d, want 404: %s", status, body)
	}
	if !strings.Contains(body, "NoSuchBucket") {
		t.Errorf("the refusal does not name NoSuchBucket: %s", body)
	}
}

// TestListObjectsV1RefusesTheVersionsSubresource keeps ?versions from being
// answered as a plain listing: a client that asked for object versions and got
// a key list would conclude that versioning works and that nothing has ever been
// overwritten.
func TestListObjectsV1RefusesTheVersionsSubresource(t *testing.T) {
	ts := newStoreServer(t, storage.NewMemoryStore())
	createBucketOverWire(t, ts, "versioned")

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/versioned?versions", nil)
	if err != nil {
		t.Fatalf("build list: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "NotImplemented") {
		t.Errorf("the refusal does not name NotImplemented: %s", body)
	}
}
