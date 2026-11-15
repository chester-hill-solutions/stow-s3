//go:build linux

package conformance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	resume "github.com/chester-hill-solutions/stow-s3/examples/resumable-transfer"
)

type costEvidence struct {
	Schema                     string `json:"schema"`
	Direction                  string `json:"direction"`
	Profile                    string `json:"profile"`
	SizeBytes                  int    `json:"sizeBytes"`
	MaxBytes                   int64  `json:"maxBytes"`
	RangeBytes                 int    `json:"rangeBytes,omitempty"`
	PartBytes                  int    `json:"partBytes,omitempty"`
	RestartAfterFirstRange     bool   `json:"restartAfterFirstRange,omitempty"`
	RestartAfterLostPart       bool   `json:"restartAfterLostPart,omitempty"`
	RestartAfterLostCompletion bool   `json:"restartAfterLostCompletion,omitempty"`
	RangeGetCount              int    `json:"rangeGetCount,omitempty"`
	AllGetRequestCount         int    `json:"allGetRequestCount,omitempty"`
	RangeBytesRead             int64  `json:"rangeBytesRead,omitempty"`
	RangeRetries               int    `json:"rangeRetries"`
	Part1Attempts              int    `json:"part1Attempts,omitempty"`
	Part2Attempts              int    `json:"part2Attempts,omitempty"`
	PartPayloadBytesSent       int64  `json:"partPayloadBytesSent,omitempty"`
	PartPayloadScope           string `json:"partPayloadScope,omitempty"`
	ListPages                  int    `json:"listPages,omitempty"`
	CompletionAttempts         int    `json:"completionAttempts,omitempty"`
	FinalBytes                 int    `json:"finalBytes"`
	FinalSHA256                string `json:"finalSHA256"`
	TotalAllocDeltaBytes       uint64 `json:"totalAllocDeltaBytes"`
	MallocsDelta               uint64 `json:"mallocsDelta"`
	AllocationScope            string `json:"allocationScope"`
	MaxRSSBytes                int64  `json:"maxRssBytes"`
	RSSScope                   string `json:"rssScope"`
	ProcReadBytes              int64  `json:"procReadBytes"`
	ProcWriteBytes             int64  `json:"procWriteBytes"`
	ProcIOScope                string `json:"procIOScope"`
	StorageApparentBytes       int64  `json:"storageApparentBytes"`
	StorageAllocatedBytes      int64  `json:"storageAllocatedBytes"`
	StorageScope               string `json:"storageScope"`
	ClientFilesApparentBytes   int64  `json:"clientFilesApparentBytes,omitempty"`
	ClientFilesAllocatedBytes  int64  `json:"clientFilesAllocatedBytes,omitempty"`
	UploadStateBytes           int64  `json:"uploadStateBytes,omitempty"`
	DurationMs                 int64  `json:"durationMs"`
	GoVersion                  string `json:"goVersion"`
}

type costFixture struct {
	profile  string
	host     *resumeHost
	target   resume.Target
	source   string
	data     []byte
	download resume.Downloader
	upload   resume.Uploader
}

func TestResumeDownloadCostEvidence(t *testing.T) {
	profile, out := costOptions(t)
	f := newCostFixture(t, profile)
	f.download = resumeDownloader(t, f.host, f.target, f.data)
	started, before := startCostSample()
	progress := f.downloadToCompletion(t)
	final := readTransferFile(t, f.download.Path)
	f.verifyFinal(t, final)
	ranges := f.host.wire.rangeSnapshot()
	assertDownloadRanges(t, ranges, progress.ETag, len(f.data))
	if f.host.wire.getRequestCount() != len(ranges) {
		t.Fatal("download made a GET outside the verified range sequence")
	}
	evidence := f.evidence(t, before, started, final)
	evidence.Direction = "download"
	evidence.RangeBytes = resumeDownloadChunk
	evidence.RestartAfterFirstRange = profile != "memory"
	evidence.RangeGetCount = len(ranges)
	evidence.AllGetRequestCount = f.host.wire.getRequestCount()
	for _, record := range ranges {
		evidence.RangeBytesRead += record.BytesRead
	}
	evidence.ClientFilesApparentBytes, evidence.ClientFilesAllocatedBytes = footprint(t, filepath.Dir(f.download.Path))
	writeCostEvidence(t, out, evidence)
}

func TestResumeUploadCostEvidence(t *testing.T) {
	profile, out := costOptions(t)
	f := newCostFixture(t, profile)
	f.upload = resumeUploader(t, f.host, f.target)
	started, before := startCostSample()
	f.uploadToCompletion(t)
	final := f.readUploaded(t)
	f.verifyFinal(t, final)
	f.verifyUploadAccounting(t)
	evidence := f.evidence(t, before, started, final)
	evidence.Direction = "upload"
	evidence.PartBytes = 5 << 20
	evidence.RestartAfterLostPart = profile != "memory"
	evidence.RestartAfterLostCompletion = profile != "memory"
	evidence.Part1Attempts = f.host.wire.parts[1]
	evidence.Part2Attempts = f.host.wire.parts[2]
	evidence.PartPayloadBytesSent = f.host.wire.partBytes
	evidence.PartPayloadScope = "Sum of decoded UploadPart request payload lengths; excludes HTTP framing, error bodies and TCP retransmissions"
	evidence.ListPages = f.host.wire.listPages
	evidence.CompletionAttempts = f.host.wire.completions
	state, err := os.Stat(f.upload.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	evidence.UploadStateBytes = state.Size()
	writeCostEvidence(t, out, evidence)
}

func costOptions(t *testing.T) (string, string) {
	t.Helper()
	profile := os.Getenv("STOW_RESUME_COST_PROFILE")
	if profile == "" {
		t.Skip("set STOW_RESUME_COST_PROFILE to opt in")
	}
	switch profile {
	case "memory", "filesystem", "runtimefs", "workspace":
	default:
		t.Fatalf("invalid profile %q", profile)
	}
	out := os.Getenv("STOW_RESUME_COST_OUT")
	if out == "" || !filepath.IsAbs(out) {
		t.Fatal("STOW_RESUME_COST_OUT must be an exact absolute JSON output path")
	}
	return profile, out
}

func newCostFixture(t *testing.T, profile string) *costFixture {
	t.Helper()
	source, data := resumeSource(t)
	return &costFixture{profile: profile, host: newResumeHost(t, profile),
		target: resume.Target{Bucket: uniqueBucket(t), Key: "cost-evidence"}, source: source, data: data}
}

func startCostSample() (time.Time, runtime.MemStats) {
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	return time.Now(), before
}

func (f *costFixture) restart() {
	if f.profile != "memory" {
		f.host.restart()
	}
}

func (f *costFixture) downloadToCompletion(t *testing.T) resume.Progress {
	t.Helper()
	started, err := f.download.Start(t.Context(), f.target, sourceSHA(f.data))
	if err != nil {
		t.Fatal(err)
	}
	progress, err := f.download.Resume(t.Context(), resumeDownloadChunk)
	if err != nil || progress.Offset != resumeDownloadChunk || progress.ETag != started.ETag {
		t.Fatalf("first range: %+v, %v", progress, err)
	}
	f.restart()
	f.download.Client = f.host.client()
	return completeDownload(t, f.download, progress)
}

func (f *costFixture) uploadToCompletion(t *testing.T) {
	t.Helper()
	if _, err := f.upload.Start(t.Context(), f.source, f.target); err != nil {
		t.Fatal(err)
	}
	f.host.wire.dropPart = true
	if _, err := f.upload.Resume(t.Context(), f.source, 1); err == nil {
		t.Fatal("lost part reply was not surfaced")
	}
	f.restart()
	f.upload.Client = f.host.client()
	if _, err := f.upload.Resume(t.Context(), f.source, 1); err != nil {
		t.Fatal(err)
	}
	f.host.wire.dropComplete = true
	if _, err := f.upload.Resume(t.Context(), f.source, 0); err == nil {
		t.Fatal("lost completion reply was not surfaced")
	}
	f.restart()
	f.upload.Client = f.host.client()
	progress, err := f.upload.Resume(t.Context(), f.source, 0)
	if err != nil || progress.Phase != "complete" {
		t.Fatalf("completion resolve=%+v,%v", progress, err)
	}
}

func (f *costFixture) readUploaded(t *testing.T) []byte {
	t.Helper()
	object, err := f.host.client().GetObject(t.Context(), &s3.GetObjectInput{Bucket: &f.target.Bucket, Key: &f.target.Key})
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(object.Body)
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read verification: %v, close: %v", readErr, closeErr)
	}
	return data
}

func (f *costFixture) verifyFinal(t *testing.T, final []byte) {
	t.Helper()
	if !bytes.Equal(final, f.data) || sourceSHA(final) != sourceSHA(f.data) {
		t.Fatal("final bytes or SHA-256 mismatch")
	}
}

func (f *costFixture) verifyUploadAccounting(t *testing.T) {
	t.Helper()
	wire := f.host.wire
	if len(wire.parts) != 2 || wire.parts[1] != 1 || wire.parts[2] != 1 || wire.partBytes != int64(len(f.data)) {
		t.Fatalf("upload part accounting: attempts=%v payload=%d", wire.parts, wire.partBytes)
	}
	if wire.completions != 1 || wire.listPages < 4 {
		t.Fatalf("completions=%d list pages=%d", wire.completions, wire.listPages)
	}
}

func (f *costFixture) evidence(t *testing.T, before runtime.MemStats, started time.Time, final []byte) costEvidence {
	t.Helper()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	elapsed := time.Since(started).Milliseconds()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	read, write := procIO(t)
	apparent, allocated := footprint(t, f.host.dir)
	return costEvidence{
		Schema: "stow.resume-cost.v1", Profile: f.profile, SizeBytes: len(f.data), MaxBytes: 32 << 20,
		FinalBytes: len(final), FinalSHA256: sourceSHA(final),
		TotalAllocDeltaBytes: after.TotalAlloc - before.TotalAlloc, MallocsDelta: after.Mallocs - before.Mallocs,
		AllocationScope: "Process-wide Go allocations after fixture, host and SDK seed setup through final verification; combines client and server; not peak heap",
		MaxRSSBytes:     usage.Maxrss * 1024, RSSScope: "Linux process high-water RSS includes setup and in-process client/server",
		ProcReadBytes: read, ProcWriteBytes: write, ProcIOScope: "Cumulative process /proc/self/io counters include source/SDK fixture setup",
		StorageApparentBytes: apparent, StorageAllocatedBytes: allocated,
		StorageScope: "Final regular files in backend root on container ephemeral storage; excludes source fixture and client files",
		DurationMs:   elapsed, GoVersion: runtime.Version(),
	}
}

func writeCostEvidence(t *testing.T, path string, evidence costEvidence) {
	t.Helper()
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}

func procIO(t *testing.T) (int64, int64) {
	t.Helper()
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Fatal(err)
	}
	var read, write int64
	foundRead, foundWrite := false, false
	for _, line := range strings.Split(string(data), "\n") {
		var n int64
		if _, err := fmt.Sscanf(line, "read_bytes: %d", &n); err == nil {
			read = n
			foundRead = true
		}
		if _, err := fmt.Sscanf(line, "write_bytes: %d", &n); err == nil {
			write = n
			foundWrite = true
		}
	}
	if !foundRead || !foundWrite {
		t.Fatal("/proc/self/io lacks required byte counters")
	}
	return read, write
}

func footprint(t *testing.T, root string) (int64, int64) {
	t.Helper()
	var apparent, allocated int64
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected symlink in measured footprint: %s", path)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		apparent += info.Size()
		stat, ok := info.Sys().(*syscall.Stat_t)
		if ok {
			allocated += stat.Blocks * 512
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return apparent, allocated
}
