package storage

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash"
	"hash/crc32"
	"hash/crc64"
	"io"
	"strings"
)

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)
var crc64NVMETable = crc64.MakeTable(0x9a6c9329ac4bc9b5)

// ComputeChecksum returns the S3 base64 checksum for a supported algorithm.
func ComputeChecksum(algorithm string, data []byte) (string, error) {
	return ComputeChecksumReader(algorithm, bytes.NewReader(data))
}

func ComputeChecksumReader(algorithm string, input io.Reader) (string, error) {
	digest, err := NewChecksumDigest(algorithm)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(digest, input); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(digest.Sum(nil)), nil
}

func NewChecksumDigest(algorithm string) (hash.Hash, error) {
	switch NormalizeChecksumAlgorithm(algorithm) {
	case "CRC32":
		return crc32.NewIEEE(), nil
	case "CRC32C":
		return crc32.New(crc32cTable), nil
	case "CRC64NVME":
		return crc64.New(crc64NVMETable), nil
	case "SHA1":
		return sha1.New(), nil
	case "SHA256":
		return sha256.New(), nil
	default:
		return nil, fmt.Errorf("unsupported checksum algorithm %q", algorithm)
	}
}

func NormalizeChecksumAlgorithm(algorithm string) string {
	return strings.ToUpper(strings.TrimSpace(algorithm))
}

// VerifyChecksum checks a caller-supplied checksum against the body it was
// supplied for, and returns ErrChecksumMismatch when they disagree.
//
// It is package-level rather than a method on one store because verifying an
// integrity claim is part of the object model, and it had ended up implemented in
// exactly one of the three stores: workspace verified, memory and filesystem
// stored the value verbatim and accepted a corrupt body.
//
// A caller who supplies neither an algorithm nor a value has made no claim, so
// there is nothing to verify. Supplying one without the other is an error
// rather than a silent pass: storing a body whose checksum was never computed
// would advertise an integrity property the store does not have.
func VerifyChecksum(opts PutOptions, data []byte) error {
	algorithm := NormalizeChecksumAlgorithm(opts.ChecksumAlgorithm)
	if algorithm == "" && opts.ChecksumValue == "" {
		return nil
	}
	if algorithm == "" || opts.ChecksumValue == "" {
		return fmt.Errorf("checksum algorithm and value must be supplied together")
	}
	computed, err := ComputeChecksum(algorithm, data)
	if err != nil {
		return err
	}
	if computed != opts.ChecksumValue {
		return ErrChecksumMismatch
	}
	return nil
}
