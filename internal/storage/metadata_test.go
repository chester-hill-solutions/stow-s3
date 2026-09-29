package storage_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestNormalizeUserMetadata(t *testing.T) {
	input := map[string]string{"X-Amz-Meta-Project": "stow", "project": "stow", "RUN": "one"}
	got, err := storage.NormalizeUserMetadata(input)
	want := map[string]string{"project": "stow", "run": "one"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %v, %v", got, err)
	}
	if input["X-Amz-Meta-Project"] != "stow" || len(input) != 3 {
		t.Fatal("input changed")
	}
	for _, invalid := range []map[string]string{
		{"X-Amz-Meta-Project": "one", "project": "two"},
		{"Project": "one", "project": "two"},
		{"x-amz-meta-": "empty"},
		{"bad key": "value"},
		{"key": "bad\r\nvalue"},
	} {
		if _, err := storage.NormalizeUserMetadata(invalid); !errors.Is(err, storage.ErrInvalidMetadata) {
			t.Errorf("%v: error = %v", invalid, err)
		}
	}
	got, err = storage.NormalizeUserMetadata(nil)
	if got != nil || err != nil {
		t.Fatalf("empty = %v, %v", got, err)
	}
}

func TestCRC64NVMEVectors(t *testing.T) {
	// AWS Go SDK checksum test vector plus the NVMe standard check string.
	for _, vector := range []struct{ body, checksum string }{
		{"", "AAAAAAAAAAA="},
		{"hello world", "jSnVw/bqjr4="},
		{"123456789", "rosUhgp5mIg="},
	} {
		got, err := storage.ComputeChecksum("CRC64NVME", []byte(vector.body))
		if err != nil || got != vector.checksum {
			t.Errorf("%q: %q, %v; want %q", vector.body, got, err, vector.checksum)
		}
	}
}
