package s3api_test

// The multipart surface at the wire: what a client is told when it finishes an
// upload.
//
// The handlers here translate a store's answer into a status code, and a
// translation is where a discarded error turns into a reported success. The
// shared request helpers live in wire_contract_test.go.

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// failingCompletionStore is a store whose completion fails while its other
// operations work, so the wire status can only come from how the handler renders
// the store's error.
//
// Both halves of the store contract are embedded because multipart is an
// optional interface the server discovers by assertion. Overriding only the
// object-model half would leave the real multipart implementation in place, and
// the test would pass or fail for the wrong reason - which is exactly what
// happened when the interface was split and this stub was not updated with it.
type failingCompletionStore struct {
	storage.Store
	storage.MultipartStore
	failure error
}

// newFailingCompletionStore wraps a real store so that only completion is
// replaced. The embedded interfaces have to be a working store: a nil one panics
// the moment the test sets an upload up, which looks like a server crash rather
// than a stub.
func newFailingCompletionStore(t *testing.T, failure error) storage.Store {
	t.Helper()
	base := storage.NewMemoryStore()
	var asStore storage.Store = base
	multi, ok := asStore.(storage.MultipartStore)
	if !ok {
		t.Fatalf("%T does not implement storage.MultipartStore", base)
	}
	return failingCompletionStore{Store: base, MultipartStore: multi, failure: failure}
}

func (s failingCompletionStore) CompleteMultipartUpload(context.Context, string, []storage.PartInfo) (*storage.ObjectMeta, error) {
	return nil, s.failure
}

// completeOverWire issues a CompleteMultipartUpload naming the given part
// numbers with a placeholder ETag each, and returns the status and body. The
// ETag is deliberately wrong: it is for reaching a refusal that happens before
// the parts are matched, which is what the part-size and store-failure cases
// are about.
func completeOverWire(t *testing.T, ts *httptest.Server, ref objectRef, uploadID string, partNumbers ...int) (int, string) {
	t.Helper()
	parts := make(map[int]string, len(partNumbers))
	for _, number := range partNumbers {
		parts[number] = `"etag"`
	}
	return completeWithETags(t, ts, ref, uploadID, parts)
}

// completeWithETags issues a CompleteMultipartUpload naming each part with its
// real ETag, and returns the status and body.
func completeWithETags(t *testing.T, ts *httptest.Server, ref objectRef, uploadID string, parts map[int]string) (int, string) {
	t.Helper()
	numbers := make([]int, 0, len(parts))
	for number := range parts {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)

	var request strings.Builder
	request.WriteString("<CompleteMultipartUpload>")
	for _, number := range numbers {
		fmt.Fprintf(&request, "<Part><PartNumber>%d</PartNumber><ETag>%s</ETag></Part>", number, parts[number])
	}
	request.WriteString("</CompleteMultipartUpload>")
	body := request.String()

	req, err := http.NewRequest(http.MethodPost, ts.URL+ref.path()+"?uploadId="+uploadID, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build complete: %v", err)
	}
	req.ContentLength = int64(len(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

// uploadPartsOverWire starts a multipart upload and uploads count parts of the
// given size, returning the upload ID and each part's ETag.
func uploadPartsOverWire(t *testing.T, ts *httptest.Server, ref objectRef, count, size int) (string, []string) {
	t.Helper()
	sizes := make([]int, count)
	for i := range sizes {
		sizes[i] = size
	}
	uploadID, etags := uploadVariableParts(t, ts, ref, sizes)
	return uploadID, etags
}

// uploadVariableParts starts a multipart upload and uploads one part per size, in
// order, returning the upload ID and each part's ETag. The parts differ in
// content, so each ETag identifies its own part rather than every part sharing
// one.
func uploadVariableParts(t *testing.T, ts *httptest.Server, ref objectRef, sizes []int) (string, []string) {
	t.Helper()
	uploadID := startUpload(t, ts, ref, nil)
	return uploadID, uploadPartsTo(t, ts, ref, uploadID, sizes)
}

// startUpload initiates an upload, optionally with headers, and returns its ID.
func startUpload(t *testing.T, ts *httptest.Server, ref objectRef, headers map[string]string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+ref.path()+"?uploads", nil)
	if err != nil {
		t.Fatalf("build create upload: %v", err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var initiated struct {
		UploadID string `xml:"UploadId"`
	}
	if err := xml.Unmarshal(data, &initiated); err != nil {
		t.Fatalf("parse create upload %q: %v", data, err)
	}
	if initiated.UploadID == "" {
		t.Fatalf("create upload returned no id: %s", data)
	}
	return initiated.UploadID
}

// uploadPartsTo uploads one part per size to an existing upload, in order,
// returning each part's ETag. Part N is filled with the Nth letter, so no two
// parts share an ETag.
func uploadPartsTo(t *testing.T, ts *httptest.Server, ref objectRef, uploadID string, sizes []int) []string {
	t.Helper()
	etags := make([]string, 0, len(sizes))
	for number, size := range sizes {
		payload := strings.Repeat(string(rune('a'+number)), size)
		req, err := http.NewRequest(http.MethodPut, ts.URL+ref.path()+"?partNumber="+strconv.Itoa(number+1)+"&uploadId="+uploadID, strings.NewReader(payload))
		if err != nil {
			t.Fatalf("build upload part: %v", err)
		}
		req.ContentLength = int64(len(payload))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("upload part %d: %v", number+1, err)
		}
		partData, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var part struct {
			ETag string `xml:"ETag"`
		}
		if err := xml.Unmarshal(partData, &part); err != nil {
			t.Fatalf("parse upload part %d %q: %v", number+1, partData, err)
		}
		etags = append(etags, part.ETag)
	}
	return etags
}

// A failure to complete is reported, not absorbed.
//
// The size check is now the store's, so this asserts the property that survives
// the move: whatever the store says about a completion is what the client is
// told, and a store that breaks is a 500 rather than a success.
func TestCompleteMultipartUploadReportsAStoreFailure(t *testing.T) {
	cases := []struct {
		name       string
		failure    error
		wantStatus int
		wantInBody string
	}{
		// A store error with a mapping is reported as that error, so a client
		// asking about an upload that is not there is told so at the point it
		// asks rather than after a wasted round trip through completion.
		{"mapped store error", storage.ErrUploadNotFound, http.StatusNotFound, "NoSuchUpload"},
		// An unmapped error is a server fault, and 500 is the honest answer: the
		// request was well formed and the store broke. What must not happen is
		// the error disappearing and the request being reported as a success.
		{"unmapped store error", errors.New("the upload directory could not be read"), http.StatusInternalServerError, "InternalError"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := newStoreServer(t, newFailingCompletionStore(t, tc.failure))
			createBucketOverWire(t, ts, "uploads")
			uploadID, _ := uploadPartsOverWire(t, ts, objectRef{"uploads", "object.bin"}, 2, 16)

			status, body := completeOverWire(t, ts, objectRef{"uploads", "object.bin"}, uploadID, 1, 2)
			if status != tc.wantStatus {
				t.Fatalf("complete = %d, want %d: %s", status, tc.wantStatus, body)
			}
			if !strings.Contains(body, tc.wantInBody) {
				t.Fatalf("complete body = %s, want it to report %s", body, tc.wantInBody)
			}
		})
	}
}

// The minimum part size is enforced on non-final parts, and the client is told
// so in S3's words: two 16-byte parts, the first of which is not final, are
// refused with EntityTooSmall.
//
// The parts are named with their real ETags. A completion whose ETags are wrong
// is refused for a different reason first, and the size rule is not what that
// refusal is about.
func TestCompleteMultipartUploadEnforcesTheMinimumPartSize(t *testing.T) {
	ts := newStoreServer(t, storage.NewMemoryStore())
	createBucketOverWire(t, ts, "uploads")
	ref := objectRef{"uploads", "object.bin"}
	uploadID, etags := uploadVariableParts(t, ts, ref, []int{16, 16})

	status, body := completeWithETags(t, ts, ref, uploadID, map[int]string{1: etags[0], 2: etags[1]})
	if status != http.StatusBadRequest {
		t.Fatalf("complete of two 16-byte parts = %d, want 400: %s", status, body)
	}
	if !strings.Contains(body, "EntityTooSmall") {
		t.Fatalf("complete body = %s, want EntityTooSmall", body)
	}
}

// The final part is exempt, as it is in S3: the minimum applies to the parts a
// client intends to keep appending to, and refusing the last one would make
// every small upload impossible. A single part is always final, so a lone
// undersized part completes.
func TestCompleteMultipartUploadExemptsTheFinalPartFromTheMinimum(t *testing.T) {
	ts := newStoreServer(t, storage.NewMemoryStore())
	createBucketOverWire(t, ts, "uploads")
	uploadID, etags := uploadPartsOverWire(t, ts, objectRef{"uploads", "object.bin"}, 1, 16)

	status, body := completeWithETags(t, ts, objectRef{"uploads", "object.bin"}, uploadID, map[int]string{1: etags[0]})
	if status != http.StatusOK {
		t.Fatalf("complete of a single small final part = %d, want 200: %s", status, body)
	}
}

// The three sizes the minimum is stated in, at the wire.
//
// One byte plus one byte is refused because the first part is not final. Five
// mebibytes plus one byte is accepted because the first part is exactly the
// minimum — the boundary belongs to the legal side. One part of one byte is
// accepted because a single part is always final.
func TestCompleteMultipartUploadMinimumPartSizeAtTheWire(t *testing.T) {
	const minPart = 5 * 1024 * 1024
	cases := []struct {
		name       string
		sizes      []int
		wantStatus int
	}{
		{"one byte plus one byte", []int{1, 1}, http.StatusBadRequest},
		{"five mebibytes plus one byte", []int{minPart, 1}, http.StatusOK},
		{"a single one-byte part", []int{1}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := newStoreServer(t, storage.NewMemoryStore())
			createBucketOverWire(t, ts, "uploads")
			ref := objectRef{"uploads", "object.bin"}
			uploadID, etags := uploadVariableParts(t, ts, ref, tc.sizes)

			parts := make(map[int]string, len(tc.sizes))
			for i := range tc.sizes {
				parts[i+1] = etags[i]
			}
			status, body := completeWithETags(t, ts, ref, uploadID, parts)
			if status != tc.wantStatus {
				t.Fatalf("complete = %d, want %d: %s", status, tc.wantStatus, body)
			}
			if tc.wantStatus == http.StatusBadRequest && !strings.Contains(body, "EntityTooSmall") {
				t.Fatalf("complete body = %s, want EntityTooSmall", body)
			}
		})
	}
}

// The properties fixed at initiation reach the completed object.
//
// A Content-Type and user metadata given to CreateMultipartUpload used to be
// dropped on the floor: the storage model kept only the upload ID, the bucket,
// the key and the initiation time, so completion had nothing to build the
// object's metadata from and published an object with neither. A client that
// uploaded a JSON document with a content type and read it back over HeadObject
// was told it was application/octet-stream with no metadata at all.
func TestCompletedObjectKeepsTheInitiationProperties(t *testing.T) {
	ts := newStoreServer(t, storage.NewMemoryStore())
	createBucketOverWire(t, ts, "uploads")
	ref := objectRef{"uploads", "document.json"}

	uploadID := startUpload(t, ts, ref, map[string]string{
		"Content-Type":   "application/json",
		"x-amz-meta-foo": "bar",
	})
	etags := uploadPartsTo(t, ts, ref, uploadID, []int{1})
	status, body := completeWithETags(t, ts, ref, uploadID, map[int]string{1: etags[0]})
	if status != http.StatusOK {
		t.Fatalf("complete = %d, want 200: %s", status, body)
	}

	head, err := http.NewRequest(http.MethodHead, ts.URL+ref.path(), nil)
	if err != nil {
		t.Fatalf("build head: %v", err)
	}
	headResp, err := http.DefaultClient.Do(head)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	defer headResp.Body.Close()
	if got := headResp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("completed object Content-Type = %q, want application/json", got)
	}
	if got := headResp.Header.Get("x-amz-meta-foo"); got != "bar" {
		t.Errorf("completed object x-amz-meta-foo = %q, want bar", got)
	}
}
