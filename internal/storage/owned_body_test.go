package storage

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
)

type borrowedBodyForPreparation struct {
	*bytes.Reader
	data []byte
}

func (b borrowedBodyForPreparation) Bytes() []byte { return b.data }

func TestPreparedPutAdoptsOnlyExplicitOwnedBodies(t *testing.T) {
	data := []byte("owned payload")
	owned := NewOwnedBody(data)
	metadata := map[string]string{"state": "original"}
	prepared, err := PreparePut(owned, PutOptions{ComputeChecksum: true, ChecksumAlgorithm: "SHA256", Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	if &prepared.Data[0] != &data[0] {
		t.Fatal("explicit ownership transfer copied its payload")
	}
	metadata["state"] = "changed"
	checksum, _ := ComputeChecksum("SHA256", data)
	if prepared.Options.Metadata["state"] != "original" || prepared.Options.ChecksumValue != checksum || prepared.ETag != ETagForBytes(data) {
		t.Fatal("transfer skipped integrity or metadata ownership")
	}
	if _, err := PreparePut(owned, PutOptions{}); !errors.Is(err, errOwnedBodyTaken) {
		t.Fatalf("body transferred twice: %v", err)
	}
	if _, err := owned.Read(make([]byte, 1)); !errors.Is(err, errOwnedBodyTaken) {
		t.Fatalf("read after transfer: %v", err)
	}
	borrowed := []byte("borrowed payload")
	copy, err := PreparePut(borrowedBodyForPreparation{bytes.NewReader(borrowed), borrowed}, PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	borrowed[0] = 'X'
	if string(copy.Data) != "borrowed payload" {
		t.Fatal("borrowed reader was adopted")
	}
}

func TestOwnedBodySupportsFallbackReadsAndEmptyTransfer(t *testing.T) {
	owned := NewOwnedBody([]byte("fallback"))
	data, err := io.ReadAll(owned)
	if err != nil || string(data) != "fallback" || owned.Len() != 0 {
		t.Fatalf("fallback read: %q %v", data, err)
	}
	empty := NewOwnedBody(nil)
	prepared, err := PreparePut(empty, PutOptions{ComputeChecksum: true, ChecksumAlgorithm: "CRC32"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Data) != 0 || prepared.Options.ChecksumValue != "AAAAAA==" {
		t.Fatal("empty transfer changed checksum")
	}
	if _, err := PreparePut(empty, PutOptions{}); !errors.Is(err, errOwnedBodyTaken) {
		t.Fatalf("empty body reused: %v", err)
	}
}

func TestOwnedBodyHasOneConcurrentRecipient(t *testing.T) {
	owned := NewOwnedBody([]byte("shared transfer"))
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		workers.Add(1)
		go func() { defer workers.Done(); _, err := PreparePut(owned, PutOptions{}); results <- err }()
	}
	workers.Wait()
	close(results)
	successful := 0
	for err := range results {
		if err == nil {
			successful++
		} else if !errors.Is(err, errOwnedBodyTaken) {
			t.Fatal(err)
		}
	}
	if successful != 1 {
		t.Fatalf("ownership recipients: %d", successful)
	}
}
