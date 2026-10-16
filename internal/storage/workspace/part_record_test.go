package workspace

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func partRecordFixture(t *testing.T, number int) ([]byte, storage.PartInfo) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "part")
	data := []byte("payload")
	part := storage.PartInfo{PartNumber: number, ETag: storage.ETagForBytes(data), Size: int64(len(data)), LastModified: time.Date(1960, 10, 16, 0, 0, 0, 123456789, time.UTC)}
	if err := writePartRecord(file, part, data); err != nil {
		t.Fatal(err)
	}
	reader, got, err := openPartRecord(file, number)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if got != part {
		t.Fatalf("descriptor=%+v want=%+v", got, part)
	}
	body, err := readPartRecord(file, number)
	if err != nil || string(body) != "payload" {
		t.Fatalf("payload=%q error=%v", body, err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return raw, part
}

func TestPartRecordRoundTrip(t *testing.T) {
	for _, number := range []int{1, 10000} {
		partRecordFixture(t, number)
	}
}

func TestPartRecordRejectsCorruption(t *testing.T) {
	raw, _ := partRecordFixture(t, 1)
	for _, kind := range []string{"short-prefix", "magic", "short-payload", "extra-payload", "part-number", "negative-size", "huge-size", "nanoseconds", "timestamp", "old-raw-part"} {
		t.Run(kind, func(t *testing.T) { checkPartRecordCorruption(t, raw, kind) })
	}
}

func corruptPartRecord(raw []byte, kind string) []byte {
	data := append([]byte(nil), raw...)
	switch kind {
	case "short-prefix":
		data = data[:partRecordPrefix-1]
	case "magic":
		data[0] = 'X'
	case "short-payload":
		data = data[:len(data)-1]
	case "extra-payload":
		data = append(data, 0)
	case "part-number":
		binary.LittleEndian.PutUint16(data[partNumberOffset:], 2)
	case "negative-size":
		binary.LittleEndian.PutUint64(data[partSizeOffset:], ^uint64(0))
	case "huge-size":
		binary.LittleEndian.PutUint64(data[partSizeOffset:], 1<<62)
	case "nanoseconds":
		binary.LittleEndian.PutUint32(data[partNsecOffset:], 1e9)
	case "timestamp":
		binary.LittleEndian.PutUint64(data[partTimeOffset:], uint64(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC).Unix()))
	case "old-raw-part":
		data = []byte("payload")
	}
	return data
}

func checkPartRecordCorruption(t *testing.T, raw []byte, kind string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "part")
	if err := os.WriteFile(file, corruptPartRecord(raw, kind), 0600); err != nil {
		t.Fatal(err)
	}
	reader, _, err := openPartRecord(file, 1)
	if err == nil {
		reader.Close()
		t.Fatal("invalid record accepted")
	}
	if reader != nil {
		reader.Close()
		t.Error("failed open returned a reader")
	}
	data, err := readPartRecord(file, 1)
	if err == nil || data != nil {
		t.Fatalf("invalid record allocated/returned payload=%d error=%v", len(data), err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
}
