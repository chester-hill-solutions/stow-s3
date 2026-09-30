package resumetransfer

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// The conformance resume suite drives a transfer against the real server and a
// client that loses replies, which is the only place the interaction can be
// seen end to end. These tests drive the same loop against a recorded wire, so
// the decisions the loop makes — how many parts to send, when to complete, what
// a vanished upload means — are asserted directly rather than inferred.

type recordedCall struct {
	method string
	part   string
	upload string
}

// recorded is a multipart endpoint that answers from memory. Parts are stored by
// number and can be withheld, which is how a reply lost after the server
// accepted the bytes is reproduced, and the finished object can be served so a
// completion is resolved by verification rather than by asking again.
// recordedPart is what the wire remembers about a part it accepted, so a
// completion can publish what was actually uploaded.
type recordedPart struct {
	etag  string
	size  int
	bytes []byte
}

type recorded struct {
	parts      map[int]recordedPart
	uploadGone bool
	object     []byte
	metadata   map[string]string
	calls      []recordedCall
}

func (w *recorded) transport() http.RoundTripper {
	return roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		w.calls = append(w.calls, recordedCall{method: request.Method, part: query.Get("partNumber"), upload: query.Get("uploadId")})
		if w.uploadGone && query.Get("uploadId") != "" {
			return xmlResponse(404, `<Error><Code>NoSuchUpload</Code><Message>gone</Message></Error>`), nil
		}
		switch {
		case request.Method == http.MethodHead && request.URL.Query().Get("uploadId") == "":
			return w.headObject()
		case request.Method == http.MethodGet && request.URL.Query().Get("uploadId") == "":
			return w.getObject(request)
		case request.Method == http.MethodGet && query.Get("uploadId") != "":
			return w.listParts(query.Get("part-number-marker")), nil
		case request.Method == http.MethodPut && query.Get("partNumber") != "":
			return w.uploadPart(request, query.Get("partNumber"))
		case request.Method == http.MethodPost && query.Get("uploadId") != "":
			return w.complete(request)
		case request.Method == http.MethodPost && query.Has("uploads"):
			// The object the upload will publish keeps the metadata the
			// initiation carried, which is what a completion is verified against.
			w.metadata = map[string]string{
				"resume-sha256":      request.Header.Get("X-Amz-Meta-Resume-Sha256"),
				"resume-transfer-id": request.Header.Get("X-Amz-Meta-Resume-Transfer-Id"),
			}
			return xmlResponse(200, `<InitiateMultipartUploadResult><Bucket>b</Bucket><Key>k</Key><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>`), nil
		}
		return xmlResponse(500, `<Error><Code>Unexpected</Code></Error>`), nil
	})
}

// listParts answers one part per page, so the loop that walks the listing is
// exercised rather than satisfied by a single response. The marker arrives
// under the wire name the SDK sends, which is not the field name.
func (w *recorded) listParts(marker string) *http.Response {
	body := `<ListPartsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`
	truncated, next := false, ""
	for number := 1; number <= len(w.parts); number++ {
		part, ok := w.parts[number]
		if !ok || strconv.Itoa(number) <= marker {
			continue
		}
		truncated = true
		next = strconv.Itoa(number)
		body += fmt.Sprintf(`<Part><PartNumber>%d</PartNumber><ETag>"%s"</ETag><Size>%d</Size></Part>`, number, part.etag, part.size)
		break
	}
	body += fmt.Sprintf(`<IsTruncated>%t</IsTruncated><NextPartNumberMarker>%s</NextPartNumberMarker></ListPartsResult>`, truncated, next)
	return xmlResponse(200, body)
}

func (w *recorded) uploadPart(request *http.Request, partNumber string) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	sum := md5.Sum(body)
	etag := hex.EncodeToString(sum[:])
	number, err := strconv.Atoi(partNumber)
	if err != nil {
		return nil, err
	}
	w.parts[number] = recordedPart{etag: etag, size: len(body), bytes: body}
	response := xmlResponse(200, fmt.Sprintf(`<UploadPartResult><ETag>"%s"</ETag></UploadPartResult>`, etag))
	response.Header.Set("ETag", `"`+etag+`"`)
	return response, nil
}

// complete publishes the object the caller assembled, so the transfer that
// finished can be verified afterwards without a second upload.
func (w *recorded) complete(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	var object []byte
	for number := 1; number <= len(w.parts); number++ {
		part, ok := w.parts[number]
		if !ok || !strings.Contains(string(body), fmt.Sprintf("<PartNumber>%d</PartNumber>", number)) {
			continue
		}
		object = append(object, part.bytes...)
	}
	w.object = object
	return xmlResponse(200, `<CompleteMultipartUploadResult><Bucket>b</Bucket><Key>k</Key><ETag>"done"</ETag></CompleteMultipartUploadResult>`), nil
}

func (w *recorded) headObject() (*http.Response, error) {
	if w.object == nil {
		return xmlResponse(404, `<Error><Code>NoSuchKey</Code></Error>`), nil
	}
	response := xmlResponse(200, "")
	response.ContentLength = int64(len(w.object))
	response.Header.Set("Content-Length", strconv.Itoa(len(w.object)))
	response.Header.Set("ETag", `"resolved"`)
	response.Header.Set("Content-Type", "application/octet-stream")
	for name, value := range w.metadata {
		response.Header.Set("x-amz-meta-"+name, value)
	}
	return response, nil
}

func (w *recorded) getObject(request *http.Request) (*http.Response, error) {
	if w.object == nil {
		return xmlResponse(404, `<Error><Code>NoSuchKey</Code></Error>`), nil
	}
	if request.Header.Get("If-Match") != `"resolved"` {
		return xmlResponse(412, `<Error><Code>PreconditionFailed</Code></Error>`), nil
	}
	response := xmlResponse(200, "")
	response.ContentLength = int64(len(w.object))
	response.Header.Set("Content-Length", strconv.Itoa(len(w.object)))
	response.Header.Set("ETag", `"resolved"`)
	response.Body = io.NopCloser(bytes.NewReader(w.object))
	return response, nil
}

func (w *recorded) count(method string) int {
	total := 0
	for _, call := range w.calls {
		if call.method == method && (method != http.MethodPost && method != http.MethodPut || call.upload != "") {
			total++
		}
	}
	return total
}

func (w *recorded) sentParts() int { return w.count(http.MethodPut) }

func (w *recorded) sentCompletions() int { return w.count(http.MethodPost) }

func xmlResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     strconv.Itoa(status),
		Header:     http.Header{"Content-Type": []string{"application/xml"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func wireUploader(t *testing.T, wire *recorded, statePath string, partSize int64) Uploader {
	t.Helper()
	client := s3.New(s3.Options{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("id", "secret", ""),
		BaseEndpoint: aws.String("http://recorded.invalid"), UsePathStyle: true,
		HTTPClient: &http.Client{Transport: wire.transport()}, RetryMaxAttempts: 1,
	})
	return Uploader{Client: client, StatePath: statePath, PartSize: partSize, MaxBytes: 64 << 20}
}

func TestResumeSendsPartsBeforeItCompletes(t *testing.T) {
	ctx := context.Background()
	wire := &recorded{parts: map[int]recordedPart{}}
	// S3 requires at least 5 MiB parts, so the parts are that size and the
	// source is three of them.
	sourcePath, _ := source(t, 3*5<<20)
	uploader := wireUploader(t, wire, filepath.Join(t.TempDir(), "upload.json"), 5<<20)
	if _, err := uploader.Start(ctx, sourcePath, Target{Bucket: "b", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	// A call that sent a part returns with the upload still open, so completion
	// is always its own step against parts that are already durable.
	first, err := uploader.Resume(ctx, sourcePath, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Phase != "uploading" || wire.sentParts() != 1 {
		t.Fatalf("after one part: phase %q, parts %d", first.Phase, wire.sentParts())
	}
	second, err := uploader.Resume(ctx, sourcePath, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.Phase != "uploading" || wire.sentParts() != 2 {
		t.Fatalf("after two parts: phase %q, parts %d", second.Phase, wire.sentParts())
	}
	third, err := uploader.Resume(ctx, sourcePath, 1)
	if err != nil {
		t.Fatal(err)
	}
	if third.Phase != "uploading" || wire.sentParts() != 3 {
		t.Fatalf("after three parts: phase %q, parts %d", third.Phase, wire.sentParts())
	}
	settled, err := uploader.Resume(ctx, sourcePath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Phase != "complete" || wire.sentParts() != 3 || wire.sentCompletions() != 1 {
		t.Fatalf("settled: phase %q, parts %d, completions %d", settled.Phase, wire.sentParts(), wire.sentCompletions())
	}
	// A settled transfer verifies rather than completing a second time, so the
	// object is never published twice.
	if _, err := uploader.Resume(ctx, sourcePath, 0); err != nil {
		t.Fatal(err)
	}
	if wire.sentCompletions() != 1 {
		t.Fatalf("completion repeated: %d", wire.sentCompletions())
	}
}

// TestResumeRefusesAnUploadThatVanished covers the two readings of a missing
// upload, which have to be told apart: work that had not finished has lost its
// remote half, and work that was mid-completion is resolved by verifying the
// object rather than by starting again.
func TestResumeRefusesAnUploadThatVanished(t *testing.T) {
	ctx := context.Background()
	wire := &recorded{parts: map[int]recordedPart{}}
	sourcePath, content := source(t, 2*5<<20)
	statePath := filepath.Join(t.TempDir(), "upload.json")
	uploader := wireUploader(t, wire, statePath, 5<<20)
	if _, err := uploader.Start(ctx, sourcePath, Target{Bucket: "b", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	if _, err := uploader.Resume(ctx, sourcePath, 1); err != nil {
		t.Fatal(err)
	}
	wire.uploadGone = true
	if _, err := uploader.Resume(ctx, sourcePath, 0); !errors.Is(err, ErrRemoteLost) {
		t.Fatalf("vanished unfinished upload = %v", err)
	}

	// Move the transfer to the phase it was in when the completion reply was
	// lost, and let the server report the object it kept.
	sha, size, err := sourceIdentity(sourcePath, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	completing := uploadProgress(size, 5<<20)
	completing.SHA256, completing.Phase = sha, "completing"
	completing.Integrity = digestState(completing)
	if err := saveState(statePath, completing); err != nil {
		t.Fatal(err)
	}
	wire.object = content
	wire.metadata = map[string]string{"resume-sha256": sha, "resume-transfer-id": completing.TransferID}
	resolved, err := uploader.Resume(ctx, sourcePath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Phase != "complete" || wire.sentCompletions() != 0 {
		t.Fatalf("resolved = %+v, completions %d", resolved, wire.sentCompletions())
	}
	// The object the server kept has to be the one this transfer was making, or
	// the completion is somebody else's work.
	wire.metadata = map[string]string{"resume-sha256": strings.Repeat("f", 64), "resume-transfer-id": completing.TransferID}
	if _, err := uploader.Resume(ctx, sourcePath, 0); !errors.Is(err, ErrChanged) {
		t.Fatalf("completion of other bytes = %v", err)
	}
	wire.object = nil
	if _, err := uploader.Resume(ctx, sourcePath, 0); err == nil {
		t.Fatal("completion verified against no object at all")
	}
}
