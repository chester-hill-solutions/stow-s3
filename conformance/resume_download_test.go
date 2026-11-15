package conformance_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	resume "github.com/chester-hill-solutions/stow-s3/examples/resumable-transfer"
)

const resumeDownloadChunk = 1 << 20

func resumeDownloader(t *testing.T, h *resumeHost, target resume.Target, data []byte) resume.Downloader {
	t.Helper()
	client := h.client()
	createBucket(t.Context(), t, client, target.Bucket)
	_, err := client.PutObject(t.Context(), &s3.PutObjectInput{
		Bucket: aws.String(target.Bucket),
		Key:    aws.String(target.Key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		t.Fatalf("seed object with SDK: %v", err)
	}
	root := t.TempDir()
	return resume.Downloader{
		Client:    client,
		StatePath: filepath.Join(root, "download.json"),
		Path:      filepath.Join(root, "download.bin"),
		MaxBytes:  32 << 20,
	}
}

func startDownloadWithPrefix(t *testing.T, h *resumeHost, target resume.Target, data []byte) (resume.Downloader, resume.Progress) {
	t.Helper()
	d := resumeDownloader(t, h, target, data)
	started, err := d.Start(t.Context(), target, sourceSHA(data))
	if err != nil {
		t.Fatalf("start download: %v", err)
	}
	p, err := d.Resume(t.Context(), resumeDownloadChunk)
	if err != nil {
		t.Fatalf("download first range: %v", err)
	}
	if p.Offset != resumeDownloadChunk {
		t.Fatalf("first range offset = %d, want %d", p.Offset, resumeDownloadChunk)
	}
	if started.ETag == "" || p.ETag != started.ETag {
		t.Fatalf("first range ETag = %q, want Start ETag %q", p.ETag, started.ETag)
	}
	if got := readTransferFile(t, d.Path); !bytes.Equal(got, data[:resumeDownloadChunk]) {
		t.Fatal("first persisted range differs from the expected object prefix")
	}
	return d, p
}

func completeDownload(t *testing.T, d resume.Downloader, p resume.Progress) resume.Progress {
	t.Helper()
	remaining := p.Size - p.Offset
	calls := int((remaining + resumeDownloadChunk - 1) / resumeDownloadChunk)
	for range calls {
		wantOffset := min(p.Offset+resumeDownloadChunk, p.Size)
		next, err := d.Resume(t.Context(), resumeDownloadChunk)
		if err != nil {
			t.Fatalf("resume at offset %d: %v", p.Offset, err)
		}
		if next.Offset != wantOffset {
			t.Fatalf("resume advanced offset from %d to %d, want %d", p.Offset, next.Offset, wantOffset)
		}
		p = next
	}
	if p.Phase != "complete" || p.Offset != p.Size {
		t.Fatalf("final progress = %+v, want complete at size %d", p, p.Size)
	}
	return p
}

func readTransferFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertTransferFilesUnchanged(t *testing.T, d resume.Downloader, beforeData, beforeState []byte) {
	t.Helper()
	if !bytes.Equal(readTransferFile(t, d.Path), beforeData) || !bytes.Equal(readTransferFile(t, d.StatePath), beforeState) {
		t.Fatal("refusal changed the saved prefix or progress state")
	}
}

func assertS3ErrorCode(t *testing.T, err error, want ...string) {
	t.Helper()
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want S3 API error %v", err, want)
	}
	for _, code := range want {
		if apiErr.ErrorCode() == code {
			return
		}
	}
	t.Fatalf("S3 error code = %q, want one of %v", apiErr.ErrorCode(), want)
}

func TestResumeDownloadRetainedRestartNoPrefixResend(t *testing.T) {
	for _, profile := range []string{"filesystem", "runtimefs", "workspace"} {
		t.Run(profile, func(t *testing.T) { resumeRetainedDownload(t, profile) })
	}
}

func resumeRetainedDownload(t *testing.T, profile string) {
	t.Helper()
	h := newResumeHost(t, profile)
	target := resume.Target{Bucket: uniqueBucket(t), Key: "download"}
	_, data := resumeSource(t)
	d, p := startDownloadWithPrefix(t, h, target, data)
	dataPath, statePath := d.Path, d.StatePath
	wantETag := p.ETag

	h.restart()
	d = resume.Downloader{Client: h.client(), StatePath: statePath, Path: dataPath, MaxBytes: 32 << 20}
	p = completeDownload(t, d, p)
	if got := readTransferFile(t, dataPath); !bytes.Equal(got, data) || sourceSHA(got) != sourceSHA(data) {
		t.Fatal("restarted download bytes or digest do not match the SDK-seeded object")
	}

	beforeGets := h.wire.getRequestCount()
	d = resume.Downloader{Client: h.client(), StatePath: statePath, Path: dataPath, MaxBytes: 32 << 20}
	p, err := d.Resume(t.Context(), resumeDownloadChunk)
	if err != nil || p.Phase != "complete" {
		t.Fatalf("resume completed download = %+v, %v", p, err)
	}
	if afterGets := h.wire.getRequestCount(); afterGets != beforeGets {
		t.Fatalf("completed reopen issued another GET: count %d -> %d", beforeGets, afterGets)
	}
	assertDownloadRanges(t, h.wire.rangeSnapshot(), wantETag, len(data))
}

func TestResumeDownloadMemoryRestartDoesNotClaimRetention(t *testing.T) {
	h := newResumeHost(t, "memory")
	target := resume.Target{Bucket: uniqueBucket(t), Key: "download"}
	_, data := resumeSource(t)
	d, _ := startDownloadWithPrefix(t, h, target, data)
	beforeData, beforeState := readTransferFile(t, d.Path), readTransferFile(t, d.StatePath)

	h.restart()
	d = resume.Downloader{Client: h.client(), StatePath: d.StatePath, Path: d.Path, MaxBytes: 32 << 20}
	_, err := d.Resume(t.Context(), resumeDownloadChunk)
	assertS3ErrorCode(t, err, "NoSuchBucket", "NoSuchKey")
	assertTransferFilesUnchanged(t, d, beforeData, beforeState)
	assertMissingRemoteRange(t, h.wire.rangeSnapshot())
}

func TestResumeDownloadRefusesOverwrittenRemoteAfterRestart(t *testing.T) {
	h := newResumeHost(t, "filesystem")
	target := resume.Target{Bucket: uniqueBucket(t), Key: "download"}
	_, data := resumeSource(t)
	d, p := startDownloadWithPrefix(t, h, target, data)
	wantETag := p.ETag
	beforeData, beforeState := readTransferFile(t, d.Path), readTransferFile(t, d.StatePath)

	h.restart()
	client := h.client()
	_, err := client.PutObject(t.Context(), &s3.PutObjectInput{
		Bucket: aws.String(target.Bucket),
		Key:    aws.String(target.Key),
		Body:   bytes.NewReader(bytes.Repeat([]byte("Z"), len(data))),
	})
	if err != nil {
		t.Fatalf("overwrite object through SDK: %v", err)
	}
	d = resume.Downloader{Client: client, StatePath: d.StatePath, Path: d.Path, MaxBytes: 32 << 20}
	_, err = d.Resume(t.Context(), resumeDownloadChunk)
	assertS3ErrorCode(t, err, "PreconditionFailed")
	assertTransferFilesUnchanged(t, d, beforeData, beforeState)
	assertOverwriteRange(t, h.wire.rangeSnapshot(), wantETag)
}

func assertMissingRemoteRange(t *testing.T, ranges []resumeRangeRecord) {
	t.Helper()
	if len(ranges) != 2 || ranges[1].Status != 404 || ranges[1].BytesRead != 0 {
		t.Fatalf("memory restart range responses = %+v, want 404 and no partial-content payload", ranges)
	}
}

func assertOverwriteRange(t *testing.T, ranges []resumeRangeRecord, wantETag string) {
	t.Helper()
	wantRange := fmt.Sprintf("bytes=%d-%d", resumeDownloadChunk, 2*resumeDownloadChunk-1)
	if len(ranges) != 2 {
		t.Fatalf("range requests = %+v, want initial read and one refused continuation", ranges)
	}
	got := ranges[1]
	if got.RequestRange != wantRange || got.IfMatch != wantETag || got.Status != 412 || got.BytesRead != 0 {
		t.Fatalf("overwritten-object request/response = %+v, want old If-Match, suffix, 412 and no partial-content payload", got)
	}
}

func assertDownloadRanges(t *testing.T, ranges []resumeRangeRecord, expectedETag string, size int) {
	t.Helper()
	expectedCount := (size + resumeDownloadChunk - 1) / resumeDownloadChunk
	if len(ranges) != expectedCount {
		t.Fatalf("range GET count = %d, want %d: %+v", len(ranges), expectedCount, ranges)
	}
	var total int64
	for i, got := range ranges {
		start := int64(i * resumeDownloadChunk)
		end := min(int64(size)-1, start+resumeDownloadChunk-1)
		wantRange := fmt.Sprintf("bytes=%d-%d", start, end)
		wantContentRange := fmt.Sprintf("bytes %d-%d/%d", start, end, size)
		if got.RequestRange != wantRange {
			t.Errorf("range %d request = %q, want %q", i, got.RequestRange, wantRange)
		}
		if got.IfMatch != expectedETag || got.ETag != expectedETag {
			t.Errorf("range %d validator mismatch: request If-Match=%q response ETag=%q expected start ETag=%q", i, got.IfMatch, got.ETag, expectedETag)
		}
		if got.Status != 206 || got.ContentRange != wantContentRange || got.Length != end-start+1 || got.BytesRead != end-start+1 {
			t.Errorf("range %d response = %+v, want 206 %q length/read %d", i, got, wantContentRange, end-start+1)
		}
		total += got.BytesRead
	}
	if total != int64(size) {
		t.Errorf("range response transferred %d bytes, want exactly %d (no prefix resend)", total, size)
	}
}
