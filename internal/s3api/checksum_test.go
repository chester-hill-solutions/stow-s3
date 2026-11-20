package s3api_test

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestPutObjectChecksumAlgorithms(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	create := func(bucket string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/"+bucket, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create bucket status = %d", resp.StatusCode)
		}
	}
	create("checksum-bucket")
	body := "checksum-body"
	for _, algorithm := range []string{"CRC32", "CRC32C", "CRC64NVME", "SHA1", "SHA256"} {
		t.Run(algorithm, func(t *testing.T) {
			value, err := storage.ComputeChecksum(algorithm, []byte(body))
			if err != nil {
				t.Fatal(err)
			}
			key := strings.ToLower(algorithm)
			req, _ := http.NewRequest(http.MethodPut, ts.URL+"/checksum-bucket/"+key, strings.NewReader(body))
			req.Header.Set("x-amz-checksum-"+strings.ToLower(algorithm), value)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				payload, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d: %s", resp.StatusCode, payload)
			}
			if got := resp.Header.Get("x-amz-checksum-" + strings.ToLower(algorithm)); got != value {
				t.Fatalf("response checksum = %q, want %q", got, value)
			}
		})
	}
}

func TestPutObjectRejectsBadAndUnknownChecksums(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/checksum-bucket", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	bad := base64.StdEncoding.EncodeToString([]byte{0, 0, 0, 0})
	for _, testCase := range []struct {
		name   string
		header string
		value  string
	}{
		{name: "bad NVME digest", header: "x-amz-checksum-crc64nvme", value: "AAAAAAAAAAA="},
		{name: "bad digest", header: "x-amz-checksum-crc32", value: bad},
		{name: "unknown algorithm", header: "x-amz-checksum-sha512", value: bad},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPut, ts.URL+"/checksum-bucket/"+testCase.name, strings.NewReader("body"))
			req.Header.Set(testCase.header, testCase.value)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestCRC64MultipartFullObjectChecksum(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	createBucketForChecksumTest(t, ts)
	uploadID := initiateCRC64Upload(t, ts)
	payload := "multipart full object checksum"
	want, err := storage.ComputeChecksum("CRC64NVME", []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	etag := uploadCRC64Part(t, ts, uploadID, payload)

	mismatched := completeCRC64(t, ts, uploadID, etag, "AAAAAA==")
	mismatched.Body.Close()
	if mismatched.StatusCode != http.StatusBadRequest {
		t.Fatalf("mismatched completion status=%d", mismatched.StatusCode)
	}

	completed := completeCRC64(t, ts, uploadID, etag, want)
	completed.Body.Close()
	if completed.StatusCode != http.StatusOK {
		t.Fatalf("completion status=%d", completed.StatusCode)
	}
	if completed.Header.Get("x-amz-checksum-crc64nvme") != want || completed.Header.Get("x-amz-checksum-type") != "FULL_OBJECT" {
		t.Fatalf("completion checksum headers = %q %q", completed.Header.Get("x-amz-checksum-crc64nvme"), completed.Header.Get("x-amz-checksum-type"))
	}
	if got := headCRC64(t, ts); got != want {
		t.Fatalf("HEAD checksum = %q, want %q", got, want)
	}
}

func createBucketForChecksumTest(t *testing.T, ts *httptest.Server) {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPut, ts.URL+"/checksum-bucket", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func initiateCRC64Upload(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPost, ts.URL+"/checksum-bucket/object?uploads", nil)
	request.Header.Set("x-amz-checksum-algorithm", "CRC64NVME")
	request.Header.Set("x-amz-checksum-type", "FULL_OBJECT")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("initiate status=%d body=%s", response.StatusCode, body)
	}
	uploadID := between(string(body), "<UploadId>", "</UploadId>")
	if uploadID == "" {
		t.Fatalf("no upload id in %s", body)
	}
	return uploadID
}

func uploadCRC64Part(t *testing.T, ts *httptest.Server, uploadID, payload string) string {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPut, ts.URL+"/checksum-bucket/object?partNumber=1&uploadId="+uploadID, strings.NewReader(payload))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("ETag") == "" {
		t.Fatalf("upload part status=%d etag=%q", response.StatusCode, response.Header.Get("ETag"))
	}
	return response.Header.Get("ETag")
}

func completeCRC64(t *testing.T, ts *httptest.Server, uploadID, etag, value string) *http.Response {
	t.Helper()
	document := "<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>" + etag + "</ETag></Part></CompleteMultipartUpload>"
	request, _ := http.NewRequest(http.MethodPost, ts.URL+"/checksum-bucket/object?uploadId="+uploadID, strings.NewReader(document))
	request.Header.Set("x-amz-checksum-crc64nvme", value)
	request.Header.Set("x-amz-checksum-type", "FULL_OBJECT")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func headCRC64(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	request, _ := http.NewRequest(http.MethodHead, ts.URL+"/checksum-bucket/object", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return response.Header.Get("x-amz-checksum-crc64nvme")
}

func between(value, start, end string) string {
	_, rest, ok := strings.Cut(value, start)
	if !ok {
		return ""
	}
	result, _, ok := strings.Cut(rest, end)
	if !ok {
		return ""
	}
	return result
}

func TestPutComputesRequestedChecksumWithoutSuppliedValue(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	request, _ := http.NewRequest(http.MethodPut, ts.URL+"/auto-checksum", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	body := "owned checksum payload"
	request, _ = http.NewRequest(http.MethodPut, ts.URL+"/auto-checksum/key", strings.NewReader(body))
	request.Header.Set("x-amz-sdk-checksum-algorithm", "CRC32C")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	expected, err := storage.ComputeChecksum("CRC32C", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("x-amz-checksum-crc32c") != expected {
		t.Fatalf("status=%d checksum=%q", response.StatusCode, response.Header.Get("x-amz-checksum-crc32c"))
	}
}
