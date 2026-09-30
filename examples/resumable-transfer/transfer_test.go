package resumetransfer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The wire behaviour of a transfer is exercised end to end by the conformance
// resume suite, which drives it through a real server and a client that loses
// replies. What is tested here is the part that decides whether a transfer may
// resume at all: the retained state, its integrity, and the part arithmetic both
// directions agree on.

func source(t *testing.T, size int) (string, []byte) {
	t.Helper()
	content := bytes.Repeat([]byte("A"), size)
	path := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, content
}

func uploadProgress(size, partSize int64) Progress {
	progress := Progress{
		Version: 1, Kind: "upload", Target: Target{Bucket: "reports", Key: "result"},
		Size: size, SHA256: strings.Repeat("a", 64), PartSize: partSize,
		UploadID: "upload-1", TransferID: strings.Repeat("b", 32), Phase: "uploading",
	}
	progress.Integrity = digestState(progress)
	return progress
}

func TestRetainedStateRoundTripsAndDetectsTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upload.json")
	progress := uploadProgress(12<<20, 5<<20)
	if err := saveState(path, progress); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadState(path, "upload")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != progress {
		t.Fatalf("retained state = %+v", loaded)
	}
	// The kind is part of the state, so a download's record cannot be resumed
	// as an upload even when both are well formed.
	if _, err := loadState(path, "download"); !errors.Is(err, ErrProgress) {
		t.Fatalf("state of another kind = %v", err)
	}
	for name, rewrite := range map[string]func(){
		"edited target":  func() { replace(t, path, `"Key":"result"`, `"Key":"other"`) },
		"edited digest":  func() { replace(t, path, progress.SHA256, strings.Repeat("c", 64)) },
		"edited version": func() { replace(t, path, `"version":1`, `"version":2`) },
		"short transfer": func() { replace(t, path, progress.TransferID, progress.TransferID[:31]) },
		"not json":       func() { replace(t, path, "{", "{{") },
	} {
		if err := saveState(path, progress); err != nil {
			t.Fatal(err)
		}
		rewrite()
		if _, err := loadState(path, "upload"); !errors.Is(err, ErrProgress) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// An absent state file reads as absent rather than corrupt, so a caller can
	// tell "start one" from "this one is damaged".
	if _, err := loadState(filepath.Join(t.TempDir(), "absent.json"), "upload"); !os.IsNotExist(err) {
		t.Fatalf("absent state = %v", err)
	}
}

func replace(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStartRefusesProgressItCannotReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "upload.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourcePath, _ := source(t, 64)
	uploader := Uploader{StatePath: path, PartSize: 5 << 20}
	if _, err := uploader.Start(context.Background(), sourcePath, Target{Bucket: "b", Key: "k"}); err == nil {
		t.Fatal("existing progress replaced without a decision")
	}
}

func TestStartRefusesAnImpossibleTransfer(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath, _ := source(t, 3<<20)
	for name, uploader := range map[string]Uploader{
		"part too small":  {StatePath: filepath.Join(dir, "a.json"), PartSize: 1 << 20},
		"part too large":  {StatePath: filepath.Join(dir, "b.json"), PartSize: 128 << 20},
		"oversized input": {StatePath: filepath.Join(dir, "d.json"), PartSize: 5 << 20, MaxBytes: 1024},
	} {
		if _, err := uploader.Start(ctx, sourcePath, Target{Bucket: "b", Key: "k"}); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	if _, err := (Uploader{StatePath: filepath.Join(dir, "e.json"), PartSize: 5 << 20}).Start(ctx, sourcePath, Target{}); !errors.Is(err, ErrProgress) {
		t.Fatalf("no target = %v", err)
	}
	// An empty source has no parts to complete, so it is refused rather than
	// published as an object the client could never verify.
	empty, _ := source(t, 0)
	if _, err := (Uploader{StatePath: filepath.Join(dir, "f.json"), PartSize: 5 << 20}).Start(ctx, empty, Target{Bucket: "b", Key: "k"}); !errors.Is(err, ErrProgress) {
		t.Fatalf("empty source = %v", err)
	}
}

func TestUploadProgressIsValidatedBeforeAnythingIsSent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath, _ := source(t, 64)
	if _, err := (Uploader{StatePath: filepath.Join(dir, "absent.json"), PartSize: 5 << 20}).Resume(ctx, sourcePath, 1); !os.IsNotExist(err) {
		t.Fatalf("resumed without retained progress: %v", err)
	}
	for name, progress := range map[string]Progress{
		"no upload id":   withUploadID(uploadProgress(64, 5<<20), ""),
		"short transfer": withTransferID(uploadProgress(64, 5<<20), "abc"),
		"part too small": withPartSize(uploadProgress(64, 5<<20), 1<<20),
		"zero part size": withPartSize(uploadProgress(64, 5<<20), 0),
		"unknown phase":  withPhase(uploadProgress(64, 5<<20), "finished"),
	} {
		if err := validateUpload(progress); !errors.Is(err, ErrProgress) {
			t.Fatalf("%s: %v", name, err)
		}
		path := filepath.Join(dir, "held.json")
		if err := saveState(path, progress); err != nil {
			t.Fatal(err)
		}
		if _, err := (Uploader{StatePath: path, PartSize: 5 << 20}).Resume(ctx, sourcePath, 1); err == nil {
			t.Fatalf("%s: resumed", name)
		}
	}
	// The recorded digest has to match the source, or the transfer would
	// complete somebody else's bytes.
	mismatched := uploadProgress(64, 5<<20)
	mismatched.SHA256 = strings.Repeat("d", 64)
	mismatched.Integrity = digestState(mismatched)
	path := filepath.Join(dir, "changed.json")
	if err := saveState(path, mismatched); err != nil {
		t.Fatal(err)
	}
	if _, err := (Uploader{StatePath: path, PartSize: 5 << 20}).Resume(ctx, sourcePath, 1); !errors.Is(err, ErrChanged) {
		t.Fatalf("changed source = %v", err)
	}
}

func TestSourceIdentityIsBoundedAndRequiresAFile(t *testing.T) {
	sourcePath, content := source(t, 1024)
	sha, size, err := sourceIdentity(sourcePath, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if size != 1024 || !validDigest(sha) {
		t.Fatalf("identity = %s, %d", sha, size)
	}
	if _, _, err := sourceIdentity(sourcePath, 512); err == nil {
		t.Fatal("source beyond its limit accepted")
	}
	if _, _, err := sourceIdentity(t.TempDir(), 1<<20); err == nil {
		t.Fatal("directory accepted as a source")
	}
	if _, _, err := sourceIdentity(filepath.Join(t.TempDir(), "absent"), 1<<20); !os.IsNotExist(err) {
		t.Fatalf("absent source = %v", err)
	}
	if transferLimit(0) != maxTransferBytes || transferLimit(1<<40) != maxTransferBytes || transferLimit(1<<20) != 1<<20 {
		t.Fatal("transfer limit does not clamp")
	}
	if !validDigest(sha) || validDigest("abc") {
		t.Fatal("digest validation")
	}
	if got, _, err := hashReader(bytes.NewReader(content)); err != nil || len(got) != 64 {
		t.Fatalf("hashReader = %s, %v", got, err)
	}
}

func TestPartsAreReadAtTheOffsetsTheCountImplies(t *testing.T) {
	sourcePath, content := source(t, 12<<20)
	file, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	progress := uploadProgress(int64(len(content)), 5<<20)
	if partCount(progress) != 3 {
		t.Fatalf("part count = %d", partCount(progress))
	}
	var rebuilt []byte
	for number := int32(1); number <= 3; number++ {
		part, err := sourcePart(file, progress, number)
		if err != nil {
			t.Fatal(err)
		}
		rebuilt = append(rebuilt, part...)
	}
	if !bytes.Equal(rebuilt, content) {
		t.Fatal("parts do not reassemble the source")
	}
	if _, err := sourcePart(file, progress, 4); err == nil {
		t.Fatal("part past the end of the source read")
	}
}

func TestDownloadRangesAreBoundedAndOnlyTheLastOneCompletes(t *testing.T) {
	whole := bytes.Repeat([]byte("B"), 100)
	if got := rangeLast(Progress{Size: 100}, 8); got != 7 {
		t.Fatalf("bounded range = %d", got)
	}
	if got := rangeLast(Progress{Size: 100}, 1<<30); got != 99 {
		t.Fatalf("oversized request not clamped = %d", got)
	}
	if got := rangeLast(Progress{Size: 100, Offset: 90}, 8); got != 97 {
		t.Fatalf("range from an offset = %d", got)
	}
	if got := rangeLast(Progress{Size: 100, Offset: 95}, 8); got != 99 {
		t.Fatalf("final range = %d", got)
	}
	path := filepath.Join(t.TempDir(), "partial")
	if err := os.WriteFile(path, whole[:90], 0o600); err != nil {
		t.Fatal(err)
	}
	// The suffix check hashes what the file will hold once the range is
	// appended, so a prefix is never mistaken for the whole object.
	if err := verifyFinalSuffix(path, whole[90:], shaOf(whole[:90])); !errors.Is(err, ErrChanged) {
		t.Fatalf("accepted a prefix as the whole object: %v", err)
	}
	if err := verifyFinalSuffix(path, whole[90:], shaOf(whole)); err != nil {
		t.Fatalf("rejected the real suffix: %v", err)
	}
	if err := verifyFinalSuffix(filepath.Join(t.TempDir(), "absent"), whole[90:], shaOf(whole)); !os.IsNotExist(err) {
		t.Fatalf("absent destination = %v", err)
	}
}

func TestDownloadAcceptsOnlyTheRangeItAskedFor(t *testing.T) {
	whole := bytes.Repeat([]byte("B"), 100)
	dir := t.TempDir()
	path := filepath.Join(dir, "download.bin")
	// The destination is created empty, as Start leaves it, so the accepted range
	// is appended to a prefix whose digest is already recorded.
	prefix := whole[:60]
	progress := Progress{
		Version: 1, Kind: "download", Target: Target{Bucket: "reports", Key: "result"},
		Size: int64(len(whole)), SHA256: shaOf(whole), ETag: "etag-1", Phase: "downloading",
		Offset: int64(len(prefix)), PrefixSHA256: shaOf(prefix),
	}
	progress.Integrity = digestState(progress)
	etag, rangeHeader := "etag-1", "bytes 60-99/100"
	respond := func(mutate func(*rangeResponse)) {
		t.Helper()
		if err := os.WriteFile(path, prefix, 0o600); err != nil {
			t.Fatal(err)
		}
		response := rangeResponse{
			body: io.NopCloser(bytes.NewReader(whole[60:])), status: 206,
			etag: etag, contentRange: rangeHeader, length: int64(len(whole) - 60),
		}
		mutate(&response)
		downloader := Downloader{StatePath: filepath.Join(dir, "download.json"), Path: path, MaxBytes: 1 << 20}
		if err := saveState(downloader.StatePath, progress); err != nil {
			t.Fatal(err)
		}
		if _, err := downloader.acceptRange(progress, 99, response); err == nil {
			t.Fatal("accepted a response that was not the range requested")
		}
	}
	for name, mutate := range map[string]func(*rangeResponse){
		"not partial":    func(r *rangeResponse) { r.status = 200 },
		"changed etag":   func(r *rangeResponse) { r.etag = "etag-2" },
		"wrong range":    func(r *rangeResponse) { r.contentRange = "bytes 60-98/100" },
		"wrong length":   func(r *rangeResponse) { r.length = 39 },
		"short body":     func(r *rangeResponse) { r.body = io.NopCloser(bytes.NewReader(whole[60:80])) },
		"wrong contents": func(r *rangeResponse) { r.body = io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("C"), 40))) },
	} {
		respond(mutate)
		_ = name
	}
	// The range that does match is appended and recorded, and because it reaches
	// the end of the object it also has to satisfy the whole-object digest.
	downloader := Downloader{StatePath: filepath.Join(dir, "accepted.json"), Path: path, MaxBytes: 1 << 20}
	if err := os.WriteFile(path, prefix, 0o600); err != nil {
		t.Fatal(err)
	}
	good := rangeResponse{
		body: io.NopCloser(bytes.NewReader(whole[60:])), status: 206,
		etag: etag, contentRange: rangeHeader, length: int64(len(whole) - 60),
	}
	accepted, err := downloader.acceptRange(progress, 99, good)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Phase != "complete" || accepted.Offset != 100 {
		t.Fatalf("accepted progress = %+v", accepted)
	}
	retained, err := loadState(downloader.StatePath, "download")
	if err != nil {
		t.Fatal(err)
	}
	// Integrity is recomputed on save, so the comparison is about the progress
	// rather than about the digest of the two copies of it.
	accepted.Integrity, retained.Integrity = "", ""
	if retained != accepted {
		t.Fatalf("retained progress = %+v", retained)
	}
}

func TestDownloadStateRefusesAPrefixThatIsNotTheOneRecorded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "download.bin")
	statePath := filepath.Join(dir, "download.json")
	whole := bytes.Repeat([]byte("B"), 100)
	progress := Progress{
		Version: 1, Kind: "download", Target: Target{Bucket: "reports", Key: "result"},
		Size: int64(len(whole)), SHA256: shaOf(whole), ETag: "etag-1", Phase: "downloading",
		Offset: 100, PrefixSHA256: shaOf(whole),
	}
	downloader := Downloader{StatePath: statePath, Path: path, MaxBytes: 1 << 20}
	if err := os.WriteFile(path, whole, 0o600); err != nil {
		t.Fatal(err)
	}
	complete := progress
	complete.Phase = "complete"
	complete.Integrity = digestState(complete)
	if err := saveState(statePath, complete); err != nil {
		t.Fatal(err)
	}
	if _, err := downloader.downloadState(); err != nil {
		t.Fatal(err)
	}
	// A destination that no longer matches the recorded prefix is a changed
	// input, not a transfer to continue.
	torn := progress
	torn.Phase = "complete"
	torn.Offset = 50
	torn.PrefixSHA256 = shaOf(whole[:50])
	torn.Integrity = digestState(torn)
	if err := saveState(statePath, torn); err != nil {
		t.Fatal(err)
	}
	if _, err := downloader.downloadState(); !errors.Is(err, ErrChanged) {
		t.Fatalf("changed destination = %v", err)
	}
	if err := os.WriteFile(path, whole[:40], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := downloader.downloadState(); !errors.Is(err, ErrChanged) {
		t.Fatalf("truncated destination = %v", err)
	}
	unknown := progress
	unknown.Phase = "finished"
	unknown.Offset = 0
	unknown.PrefixSHA256 = ""
	unknown.Integrity = digestState(unknown)
	if err := saveState(statePath, unknown); err != nil {
		t.Fatal(err)
	}
	if _, err := downloader.downloadState(); !errors.Is(err, ErrProgress) {
		t.Fatalf("unknown phase = %v", err)
	}
}

func shaOf(content []byte) string {
	sha, _, err := hashReader(bytes.NewReader(content))
	if err != nil {
		panic(err)
	}
	return sha
}

func withUploadID(progress Progress, id string) Progress {
	progress.UploadID = id
	return progress
}

func withTransferID(progress Progress, id string) Progress {
	progress.TransferID = id
	return progress
}

func withPartSize(progress Progress, size int64) Progress {
	progress.PartSize = size
	return progress
}

func withPhase(progress Progress, phase string) Progress {
	progress.Phase = phase
	return progress
}
