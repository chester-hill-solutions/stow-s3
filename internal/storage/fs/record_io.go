package fs

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
)

func readRecordHeader(file *os.File) (objectRecord, int64, error) {
	info, err := file.Stat()
	if err != nil {
		return objectRecord{}, 0, err
	}
	var prefix [binaryRecordPrefixSize]byte
	if _, err := file.ReadAt(prefix[:len(binaryRecordMagic)], 0); err != nil {
		return objectRecord{}, 0, err
	}
	if string(prefix[:len(binaryRecordMagic)]) != binaryRecordMagic {
		return readLegacyRecord(file, info.Size())
	}
	if _, err := file.ReadAt(prefix[len(binaryRecordMagic):], int64(len(binaryRecordMagic))); err != nil {
		return objectRecord{}, 0, err
	}
	size := int64(binary.LittleEndian.Uint32(prefix[len(binaryRecordMagic):]))
	if size > info.Size()-int64(binaryRecordPrefixSize) || size > math.MaxInt {
		return objectRecord{}, 0, fmt.Errorf("truncated object header")
	}
	header := make([]byte, int(size))
	if _, err := file.ReadAt(header, int64(binaryRecordPrefixSize)); err != nil {
		return objectRecord{}, 0, err
	}
	record, err := decodeRecordHeader(header)
	offset := int64(binaryRecordPrefixSize) + size
	if err == nil && record.payloadSize != info.Size()-offset {
		err = fmt.Errorf("object payload length does not match header")
	}
	return record, offset, err
}

func readLegacyRecord(file *os.File, size int64) (objectRecord, int64, error) {
	if size < 0 || size > math.MaxInt {
		return objectRecord{}, 0, fmt.Errorf("object record is too large")
	}
	data := make([]byte, int(size))
	if _, err := file.ReadAt(data, 0); err != nil {
		return objectRecord{}, 0, err
	}
	var record objectRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return record, 0, fmt.Errorf("decode object record: %w", err)
	}
	if record.Version != objectRecordVersion {
		return record, 0, fmt.Errorf("unsupported object record version %d", record.Version)
	}
	if record.Data == nil {
		record.Data = []byte{}
	}
	return record, -1, nil
}

func readObjectHeader(path string) (objectRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return objectRecord{}, err
	}
	defer file.Close()
	record, _, err := readRecordHeader(file)
	return record, err
}

type objectFileReader struct {
	*io.SectionReader
	file *os.File
}

func (r *objectFileReader) Close() error { return r.file.Close() }

func (r *objectFileReader) WriteTo(writer io.Writer) (int64, error) {
	position, err := r.SectionReader.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	_, offset, length := r.SectionReader.Outer()
	if position >= length {
		return 0, nil
	}
	if _, err := r.file.Seek(offset+position, io.SeekStart); err != nil {
		return 0, err
	}
	written, err := io.CopyN(writer, r.file, length-position)
	_, seekErr := r.SectionReader.Seek(written, io.SeekCurrent)
	if err == nil {
		err = seekErr
	}
	return written, err
}

func openObjectRecord(path string) (io.ReadCloser, objectRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, objectRecord{}, err
	}
	record, offset, err := readRecordHeader(file)
	if err != nil {
		file.Close()
		return nil, record, err
	}
	if offset < 0 {
		file.Close()
		return io.NopCloser(bytes.NewReader(record.Data)), record, nil
	}
	reader := &objectFileReader{SectionReader: io.NewSectionReader(file, offset, record.payloadSize), file: file}
	if runtime.GOOS != "windows" {
		return reader, record, nil
	}
	defer reader.Close()
	if record.payloadSize > math.MaxInt {
		return nil, record, fmt.Errorf("object payload is too large")
	}
	data := make([]byte, int(record.payloadSize))
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, record, err
	}
	record.Data = data
	return io.NopCloser(bytes.NewReader(data)), record, nil
}

func readObjectRecord(path string) (objectRecord, error) {
	reader, record, err := openObjectRecord(path)
	if err != nil {
		return record, err
	}
	defer reader.Close()
	if record.Data != nil {
		return record, nil
	}
	if record.payloadSize > math.MaxInt {
		return record, fmt.Errorf("object payload is too large")
	}
	record.Data = make([]byte, int(record.payloadSize))
	_, err = io.ReadFull(reader, record.Data)
	return record, err
}
