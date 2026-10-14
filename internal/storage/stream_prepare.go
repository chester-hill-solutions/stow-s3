package storage

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
)

// PreparePutTo writes uncommitted bytes while computing and verifying their digests.
func PreparePutTo(ctx context.Context, body io.Reader, destination io.Writer, opts PutOptions) (PutOptions, int64, string, error) {
	opts.ChecksumAlgorithm = NormalizeChecksumAlgorithm(opts.ChecksumAlgorithm)
	checksum, err := putDigest(opts)
	if err != nil {
		return opts, 0, "", err
	}
	etag := md5.New()
	sinks := []io.Writer{destination, etag}
	if checksum != nil {
		sinks = append(sinks, checksum)
	}
	if body == nil {
		body = &emptyPutReader{}
	}
	size, err := io.CopyBuffer(io.MultiWriter(sinks...), &contextPutReader{context: ctx, reader: body}, make([]byte, 128<<10))
	if err != nil {
		return opts, 0, "", err
	}
	if checksum != nil {
		value := base64.StdEncoding.EncodeToString(checksum.Sum(nil))
		if opts.ChecksumValue != "" && opts.ChecksumValue != value {
			return opts, 0, "", ErrChecksumMismatch
		}
		opts.ChecksumValue = value
	}
	opts.ComputeChecksum = false
	return opts, size, "\"" + hex.EncodeToString(etag.Sum(nil)) + "\"", nil
}

func putDigest(opts PutOptions) (hash.Hash, error) {
	if opts.ChecksumAlgorithm == "" {
		if opts.ChecksumValue != "" {
			return nil, fmt.Errorf("checksum value requires an algorithm")
		}
		return nil, nil
	}
	if !opts.ComputeChecksum && opts.ChecksumValue == "" {
		return nil, fmt.Errorf("checksum algorithm and value must be supplied together")
	}
	return NewChecksumDigest(opts.ChecksumAlgorithm)
}

type emptyPutReader struct{}

func (*emptyPutReader) Read([]byte) (int, error) { return 0, io.EOF }

type contextPutReader struct {
	context context.Context
	reader  io.Reader
}

func (r *contextPutReader) Read(data []byte) (int, error) {
	if r.context != nil && r.context.Err() != nil {
		return 0, r.context.Err()
	}
	return r.reader.Read(data)
}
