package conformance_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	resume "github.com/chester-hill-solutions/stow-s3/examples/resumable-transfer"
)

func resumeSource(t *testing.T) (string, []byte) {
	t.Helper()
	data := append(bytes.Repeat([]byte("A"), 5<<20), bytes.Repeat([]byte("B"), 317)...)
	path := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path, data
}
func sourceSHA(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func resumeUploader(t *testing.T, h *resumeHost, target resume.Target) resume.Uploader {
	t.Helper()
	createBucket(t.Context(), t, h.client(), target.Bucket)
	return resume.Uploader{Client: h.client(), StatePath: filepath.Join(t.TempDir(), "upload.json"), PartSize: 5 << 20, MaxBytes: 32 << 20}
}
func TestResumeUploadRetainedRestartNoResend(t *testing.T) {
	for _, profile := range []string{"filesystem", "runtimefs", "workspace"} {
		t.Run(profile, func(t *testing.T) {
			h := newResumeHost(t, profile)
			target := resume.Target{Bucket: uniqueBucket(t), Key: "upload"}
			u := resumeUploader(t, h, target)
			source, data := resumeSource(t)
			if _, err := u.Start(t.Context(), source, target); err != nil {
				t.Fatal(err)
			}
			h.wire.dropPart = true
			if _, err := u.Resume(t.Context(), source, 1); err == nil {
				t.Fatal("lost part reply was not surfaced")
			}
			h.restart()
			u.Client = h.client()
			if _, err := u.Resume(t.Context(), source, 1); err != nil {
				t.Fatal(err)
			}
			h.wire.dropComplete = true
			if _, err := u.Resume(t.Context(), source, 0); err == nil {
				t.Fatal("lost completion reply was not surfaced")
			}
			h.restart()
			u.Client = h.client()
			p, err := u.Resume(t.Context(), source, 0)
			if err != nil || p.Phase != "complete" {
				t.Fatalf("completion resolve=%+v,%v", p, err)
			}
			if h.wire.parts[1] != 1 || h.wire.parts[2] != 1 || h.wire.partBytes != int64(len(data)) || h.wire.completions != 1 || h.wire.listPages < 4 {
				t.Fatalf("wire=%+v", h.wire)
			}
			t.Logf("%s retained restart: parts=%v sent=%d bytes, list pages=%d, completions=%d", profile, h.wire.parts, h.wire.partBytes, h.wire.listPages, h.wire.completions)
		})
	}
}
func TestResumeUploadChangedSourceCorruptStateAndAbortedRemote(t *testing.T) {
	for _, change := range []string{"source", "state", "aborted", "part"} {
		t.Run(change, func(t *testing.T) {
			h := newResumeHost(t, "filesystem")
			target := resume.Target{Bucket: uniqueBucket(t), Key: "upload"}
			u := resumeUploader(t, h, target)
			source, _ := resumeSource(t)
			p, err := u.Start(t.Context(), source, target)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = u.Resume(t.Context(), source, 1); err != nil {
				t.Fatal(err)
			}
			alterUpload(t, resumeAlteration{host: h, upload: u, started: p, source: source, change: change})
			before := h.wire.partBytes
			u.Client = h.client()
			if _, err = u.Resume(t.Context(), source, 0); err == nil {
				t.Fatal("changed recovery input accepted")
			}
			if before != h.wire.partBytes {
				t.Fatal("refusal resent data")
			}
		})
	}
}

// resumeAlteration is one value rather than five parameters, so the call site
// reads as the change being made.
type resumeAlteration struct {
	host           *resumeHost
	upload         resume.Uploader
	started        resume.Progress
	source, change string
}

func alterUpload(t *testing.T, alteration resumeAlteration) {
	t.Helper()
	switch alteration.change {
	case "source":
		file, err := os.OpenFile(alteration.source, os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.WriteAt([]byte("X"), 0)
		_ = file.Close()
	case "state":
		if err := os.WriteFile(alteration.upload.StatePath, []byte(`{"version":1}`), 0600); err != nil {
			t.Fatal(err)
		}
	case "aborted":
		p := alteration.started
		_, err := alteration.host.client().AbortMultipartUpload(t.Context(), &s3.AbortMultipartUploadInput{Bucket: aws.String(p.Target.Bucket), Key: aws.String(p.Target.Key), UploadId: aws.String(p.UploadID)})
		if err != nil {
			t.Fatal(err)
		}
	case "part":
		p := alteration.started
		_, err := alteration.host.client().UploadPart(t.Context(), &s3.UploadPartInput{Bucket: aws.String(p.Target.Bucket), Key: aws.String(p.Target.Key), UploadId: aws.String(p.UploadID), PartNumber: aws.Int32(1), Body: bytes.NewReader(bytes.Repeat([]byte("X"), 5<<20))})
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestResumeUploadMemoryRestartLosesRemote(t *testing.T) {
	h := newResumeHost(t, "memory")
	target := resume.Target{Bucket: uniqueBucket(t), Key: "upload"}
	u := resumeUploader(t, h, target)
	source, _ := resumeSource(t)
	if _, err := u.Start(t.Context(), source, target); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Resume(t.Context(), source, 1); err != nil {
		t.Fatal(err)
	}
	h.restart()
	u.Client = h.client()
	createBucket(context.Background(), t, u.Client, target.Bucket)
	_, err := u.Resume(t.Context(), source, 0)
	if !errors.Is(err, resume.ErrRemoteLost) {
		t.Fatalf("memory restart=%v", err)
	}
}
