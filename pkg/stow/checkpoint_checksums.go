package stow

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type checkpointChecksum struct {
	digest   hash.Hash
	expected string
}

func checkpointChecksumDigests(manifest CheckpointManifest) (map[string][]checkpointChecksum, error) {
	result := map[string][]checkpointChecksum{}
	for _, object := range manifest.Objects {
		if object.ChecksumAlgorithm == "" {
			continue
		}
		digest, err := storage.NewChecksumDigest(object.ChecksumAlgorithm)
		if err != nil {
			return nil, err
		}
		result[object.Payload] = append(result[object.Payload], checkpointChecksum{digest: digest, expected: object.ChecksumValue})
	}
	return result, nil
}

func checkpointChecksumReader(input io.Reader, checks []checkpointChecksum) io.Reader {
	writers := make([]io.Writer, 0, len(checks))
	for _, check := range checks {
		writers = append(writers, check.digest)
	}
	if len(writers) == 0 {
		return input
	}
	return io.TeeReader(input, io.MultiWriter(writers...))
}

func verifyCheckpointChecksums(checks []checkpointChecksum) error {
	for _, check := range checks {
		if base64.StdEncoding.EncodeToString(check.digest.Sum(nil)) != check.expected {
			return fmt.Errorf("stow: checkpoint object checksum failed integrity validation")
		}
	}
	return nil
}

func verifyPortableChecksums(ctx context.Context, dir string, manifest CheckpointManifest) error {
	checks, err := checkpointChecksumDigests(manifest)
	if err != nil {
		return err
	}
	for name, digests := range checks {
		input, err := os.Open(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(io.Discard, checkpointChecksumReader(contextReader{ctx: ctx, reader: input}, digests))
		input.Close()
		if copyErr != nil {
			return copyErr
		}
		if err := verifyCheckpointChecksums(digests); err != nil {
			return err
		}
	}
	return nil
}
