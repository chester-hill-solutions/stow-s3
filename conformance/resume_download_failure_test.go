package conformance_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	resume "github.com/chester-hill-solutions/stow-s3/examples/resumable-transfer"
)

func TestResumeDownloadRejectsChangedPrefixBeforeGET(t *testing.T) {
	for _, profile := range []string{"filesystem", "runtimefs", "workspace"} {
		for _, damage := range []string{"corrupt", "truncate"} {
			t.Run(profile+"/"+damage, func(t *testing.T) {
				h := newResumeHost(t, profile)
				target := resume.Target{Bucket: uniqueBucket(t), Key: "download"}
				_, data := resumeSource(t)
				d, _ := startDownloadWithPrefix(t, h, target, data)
				prefix := append([]byte(nil), data[:resumeDownloadChunk]...)
				if damage == "corrupt" {
					prefix[len(prefix)/2] ^= 0xff
				} else {
					prefix = prefix[:len(prefix)-1]
				}
				if err := os.WriteFile(d.Path, prefix, 0o600); err != nil {
					t.Fatal(err)
				}
				assertChangedPrefixRefused(t, h, d)
			})
		}
	}
}

func TestResumeDownloadRejectsMalformedProgressBeforeGET(t *testing.T) {
	for _, profile := range []string{"filesystem", "runtimefs", "workspace"} {
		for _, damage := range []string{"checksum", "truncated-json"} {
			t.Run(profile+"/"+damage, func(t *testing.T) {
				h := newResumeHost(t, profile)
				target := resume.Target{Bucket: uniqueBucket(t), Key: "download"}
				_, data := resumeSource(t)
				d, _ := startDownloadWithPrefix(t, h, target, data)
				state := readTransferFile(t, d.StatePath)
				if damage == "checksum" {
					state = corruptProgressChecksum(t, state)
				} else {
					state = state[:len(state)/2]
				}
				if err := os.WriteFile(d.StatePath, state, 0o600); err != nil {
					t.Fatal(err)
				}
				beforeData, beforeState := readTransferFile(t, d.Path), readTransferFile(t, d.StatePath)
				beforeGETs := h.wire.getRequestCount()
				beforeRanges := len(h.wire.rangeSnapshot())
				d.Client = h.client()
				_, err := d.Resume(t.Context(), resumeDownloadChunk)
				if !errors.Is(err, resume.ErrProgress) {
					t.Fatalf("resume with %s progress = %v, want ErrProgress", damage, err)
				}
				assertTransferFilesUnchanged(t, d, beforeData, beforeState)
				assertNoNewResumeGET(t, h, beforeGETs, beforeRanges)
			})
		}
	}
}

func corruptProgressChecksum(t *testing.T, state []byte) []byte {
	t.Helper()
	marker := []byte(`"integrity":"`)
	at := bytes.Index(state, marker)
	if at < 0 || at+len(marker) >= len(state) {
		t.Fatal("progress state has no integrity checksum")
	}
	checksumByte := at + len(marker)
	if state[checksumByte] == '0' {
		state[checksumByte] = '1'
	} else {
		state[checksumByte] = '0'
	}
	return state
}

func assertChangedPrefixRefused(t *testing.T, h *resumeHost, d resume.Downloader) {
	t.Helper()
	beforeData, beforeState := readTransferFile(t, d.Path), readTransferFile(t, d.StatePath)
	beforeGETs := h.wire.getRequestCount()
	beforeRanges := len(h.wire.rangeSnapshot())
	d.Client = h.client()
	_, err := d.Resume(t.Context(), resumeDownloadChunk)
	if !errors.Is(err, resume.ErrChanged) {
		t.Fatalf("resume with changed local prefix = %v, want ErrChanged", err)
	}
	assertTransferFilesUnchanged(t, d, beforeData, beforeState)
	assertNoNewResumeGET(t, h, beforeGETs, beforeRanges)
}

func assertNoNewResumeGET(t *testing.T, h *resumeHost, beforeGETs, beforeRanges int) {
	t.Helper()
	if got := h.wire.getRequestCount(); got != beforeGETs {
		t.Fatalf("refused resume issued GET: count %d -> %d", beforeGETs, got)
	}
	if got := len(h.wire.rangeSnapshot()); got != beforeRanges {
		t.Fatalf("refused resume added range GET: count %d -> %d", beforeRanges, got)
	}
}

func TestResumeDownloadContinuesAfterInterruptedRangeBody(t *testing.T) {
	for _, profile := range []string{"filesystem", "runtimefs", "workspace"} {
		t.Run(profile, func(t *testing.T) { resumeAfterInterruptedRange(t, profile) })
	}
}

func resumeAfterInterruptedRange(t *testing.T, profile string) {
	t.Helper()
	h := newResumeHost(t, profile)
	target := resume.Target{Bucket: uniqueBucket(t), Key: "download"}
	_, data := resumeSource(t)
	d, progress := startDownloadWithPrefix(t, h, target, data)
	wantETag := progress.ETag
	dataPath, statePath := d.Path, d.StatePath
	verifiedData, verifiedState := readTransferFile(t, dataPath), readTransferFile(t, statePath)
	h.restart()
	d = resume.Downloader{Client: h.client(), StatePath: statePath, Path: dataPath, MaxBytes: 32 << 20}
	if !h.wire.interruptNextRangeBodyAfter(resumeDownloadChunk / 16) {
		t.Fatal("a range body interruption was already armed")
	}
	_, err := d.Resume(t.Context(), resumeDownloadChunk)
	if err == nil || !strings.Contains(err.Error(), "synthetic range response body interruption") {
		t.Fatalf("interrupted range resume = %v, want injected body error", err)
	}
	if len(verifiedData) != resumeDownloadChunk || !bytes.Equal(verifiedData, data[:resumeDownloadChunk]) {
		t.Fatalf("verified prefix before interruption has %d bytes or unexpected contents", len(verifiedData))
	}
	firstHistory := h.wire.rangeSnapshot()
	if len(firstHistory) != 2 || !firstHistory[1].Faulted || firstHistory[1].FaultBytes != resumeDownloadChunk/16 {
		t.Fatalf("interrupted response accounting = %+v", firstHistory)
	}
	assertTransferFilesUnchanged(t, d, verifiedData, verifiedState)

	h.restart()
	d = resume.Downloader{Client: h.client(), StatePath: statePath, Path: dataPath, MaxBytes: 32 << 20}
	progress = completeDownload(t, d, progress)
	got := readTransferFile(t, dataPath)
	if !bytes.Equal(got, data) || sourceSHA(got) != sourceSHA(data) {
		t.Fatal("restarted download after body interruption differs from the SDK-seeded object")
	}
	assertInterruptedDownloadRanges(t, h.wire.rangeSnapshot(), wantETag, len(data), resumeDownloadChunk/16)
	t.Logf("client consumed %d bytes before injected body error; verified prefix stayed %d bytes; final digest %s", firstHistory[1].FaultBytes, resumeDownloadChunk, sourceSHA(got))
}

func assertInterruptedDownloadRanges(t *testing.T, ranges []resumeRangeRecord, etag string, size int, faultBytes int64) {
	t.Helper()
	expectedCount := (size+resumeDownloadChunk-1)/resumeDownloadChunk + 1
	if len(ranges) != expectedCount {
		t.Fatalf("range GET count after retry = %d, want %d: %+v", len(ranges), expectedCount, ranges)
	}
	expect := interruptedRangeExpectation{etag: etag, size: size, faultBytes: faultBytes}
	var total int64
	for index, got := range ranges {
		total += assertInterruptedRangeRecord(t, index, got, expect)
	}
	if total != int64(size)+faultBytes {
		t.Errorf("client-consumed range bytes = %d, want object size %d plus failed partial %d", total, size, faultBytes)
	}
}

type interruptedRangeExpectation struct {
	etag       string
	size       int
	faultBytes int64
}

func assertInterruptedRangeRecord(t *testing.T, index int, got resumeRangeRecord, expect interruptedRangeExpectation) int64 {
	t.Helper()
	segment := index
	faulted := index == 1
	if index >= 2 {
		segment--
	}
	start := int64(segment * resumeDownloadChunk)
	end := min(int64(expect.size)-1, start+resumeDownloadChunk-1)
	wantRange := fmt.Sprintf("bytes=%d-%d", start, end)
	wantContentRange := fmt.Sprintf("bytes %d-%d/%d", start, end, expect.size)
	if got.RequestRange != wantRange || got.IfMatch != expect.etag || got.ETag != expect.etag {
		t.Errorf("range %d request/validator = %+v, want %q with ETag %q", index, got, wantRange, expect.etag)
	}
	if got.Status != 206 || got.ContentRange != wantContentRange || got.Length != end-start+1 {
		t.Errorf("range %d response = %+v, want 206 %q length %d", index, got, wantContentRange, end-start+1)
	}
	if faulted {
		if !got.Faulted || got.FaultAfter != expect.faultBytes || got.FaultBytes != expect.faultBytes || got.BytesRead != expect.faultBytes {
			t.Errorf("faulted range consumed %d client body bytes, record=%+v, want %d", got.BytesRead, got, expect.faultBytes)
		}
	} else if got.Faulted || got.BytesRead != end-start+1 {
		t.Errorf("completed range %d consumed %d client body bytes, want %d", index, got.BytesRead, end-start+1)
	}
	return got.BytesRead
}
