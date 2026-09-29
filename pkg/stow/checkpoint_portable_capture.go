package stow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/chester-hill-solutions/stow-s3/internal/rooted"
)

func preparePortableCapture(ctx context.Context, target captureTarget, options CheckpointOptions, files []CheckpointFile) (*portableCapture, error) {
	if !options.PortableObjects {
		return nil, nil
	}
	capture, err := scanPortableObjects(ctx, target, options, files)
	if err != nil {
		return nil, err
	}
	capture.provenance, err = captureCheckpointProvenance(target.dir)
	if err != nil {
		return nil, err
	}
	capture.options = options
	return &capture, nil
}

func portableCaptureSize(files []CheckpointFile, capture *portableCapture, options CheckpointOptions) (int64, error) {
	manifest := CheckpointManifest{Files: files}
	if capture != nil {
		manifest.Payloads = capture.payloads
	}
	total := checkpointManifestBytes(manifest)
	if capture != nil && (total > archiveByteLimit(options.MaxBytes) || int64(len(files)+len(capture.payloads)) > archiveFileLimit(options.MaxFiles)) {
		return 0, checkpointFailure("capacity_exceeded", "scan", "not_committed", fmt.Errorf("portable checkpoint exceeds capture limits"))
	}
	return total, nil
}

func finishPortableCapture(ctx context.Context, target captureTarget, stage string, capture *portableCapture, manifest *CheckpointManifest) error {
	if capture == nil {
		return nil
	}
	for _, payload := range capture.payloads {
		name := "objects/" + payload.Path
		if err := copyPortablePayload(ctx, target.dir, capture.sources[name], filepath.Join(stage, filepath.FromSlash(name)), payload); err != nil {
			return err
		}
	}
	after, err := scanPortableObjects(ctx, target, capture.options, manifest.Files)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(capture.snapshot, after.snapshot) || !reflect.DeepEqual(capture.excluded, after.excluded) {
		return checkpointFailure("workspace_changed", "verify", "not_committed", fmt.Errorf("logical objects or metadata changed during checkpoint"))
	}
	manifest.Provenance, err = captureCheckpointProvenance(target.dir)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(capture.provenance, manifest.Provenance) {
		return checkpointFailure("workspace_changed", "verify", "not_committed", fmt.Errorf("workspace provenance changed during capture"))
	}
	manifest.Version = portableCheckpointVersion
	manifest.PrimaryBucket = capture.snapshot.PrimaryBucket
	manifest.Buckets = capture.snapshot.Buckets
	manifest.Objects = capture.objects
	manifest.Payloads = capture.payloads
	manifest.Excluded = append(manifest.Excluded, capture.excluded...)
	manifest.WorkingDirectory, err = portableWorkingDirectory(target)
	if err != nil {
		return err
	}
	if err := validateCheckpointManifest(*manifest); err != nil {
		return err
	}
	return validatePortableManifestSize(*manifest)
}

func copyPortablePayload(ctx context.Context, root, source, destination string, payload CheckpointFile) error {
	relative, err := filepath.Rel(root, source)
	if err != nil {
		return err
	}
	confined, err := rooted.Open(root)
	if err != nil {
		return err
	}
	defer confined.Close()
	input, err := confined.OpenRegularFile(relative)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("stow: object payload must be a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(output, digest), io.LimitReader(contextReader{ctx: ctx, reader: input}, payload.Size+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if size != payload.Size || hex.EncodeToString(digest.Sum(nil)) != payload.SHA256 {
		return checkpointFailure("workspace_changed", "copy", "not_committed", fmt.Errorf("object payload changed during checkpoint"))
	}
	return confined.Check()
}

func validatePortableManifestSize(manifest CheckpointManifest) error {
	encoded, err := json.Marshal(checkpointArchiveHeader{FormatVersion: manifest.Version, Manifest: manifest})
	if err != nil {
		return err
	}
	if len(encoded) > maxCheckpointManifestSize {
		return fmt.Errorf("stow: portable checkpoint manifest exceeds size limit")
	}
	return nil
}
