package fs

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs/recordpb"
	"google.golang.org/protobuf/proto"
)

const binaryRecordMagic = "STOWOBJ2"
const binaryRecordPrefixSize = len(binaryRecordMagic) + 4
const binaryRecordVersion = 2

func encodeObjectRecord(record objectRecord) ([]byte, error) {
	header, err := encodeRecordPrefix(record)
	if err != nil {
		return nil, err
	}
	return append(header, record.Data...), nil
}

func encodeRecordPrefix(record objectRecord) ([]byte, error) {
	header, err := proto.MarshalOptions{Deterministic: true}.Marshal(recordHeader(record))
	if err != nil {
		return nil, err
	}
	if uint64(len(header)) > math.MaxUint32 {
		return nil, fmt.Errorf("object metadata exceeds format limit")
	}
	data := make([]byte, binaryRecordPrefixSize+len(header))
	copy(data, binaryRecordMagic)
	binary.LittleEndian.PutUint32(data[len(binaryRecordMagic):], uint32(len(header)))
	copy(data[binaryRecordPrefixSize:], header)
	return data, nil
}

func recordHeader(record objectRecord) *recordpb.ObjectHeader {
	size := record.payloadSize
	if record.Data != nil {
		size = int64(len(record.Data))
	}
	return &recordpb.ObjectHeader{
		Version: binaryRecordVersion, RecordVersion: recordText(record.RecordVersion),
		SaveRequest: recordText(record.SaveRequest), SaveMeaning: recordText(record.SaveMeaning),
		Key: recordText(record.Key), ContentType: recordText(record.ContentType), Metadata: recordMetadata(record.Metadata),
		Etag: recordText(record.ETag), ChecksumAlgorithm: recordText(record.ChecksumAlgorithm),
		ChecksumValue: recordText(record.ChecksumValue), LastModified: record.LastModified.Format(time.RFC3339Nano),
		PayloadSize: size,
	}
}

func decodeRecordHeader(data []byte) (objectRecord, error) {
	header := &recordpb.ObjectHeader{}
	if err := proto.Unmarshal(data, header); err != nil {
		return objectRecord{}, fmt.Errorf("decode object header: %w", err)
	}
	if header.Version != binaryRecordVersion || header.PayloadSize < 0 {
		return objectRecord{}, fmt.Errorf("unsupported or invalid object header version %d", header.Version)
	}
	modified, err := time.Parse(time.RFC3339Nano, header.LastModified)
	if err != nil {
		return objectRecord{}, fmt.Errorf("decode object timestamp: %w", err)
	}
	return objectRecord{Version: binaryRecordVersion, RecordVersion: header.RecordVersion,
		SaveRequest: header.SaveRequest, SaveMeaning: header.SaveMeaning, Key: header.Key,
		ContentType: header.ContentType, Metadata: header.Metadata, ETag: header.Etag,
		ChecksumAlgorithm: header.ChecksumAlgorithm, ChecksumValue: header.ChecksumValue,
		LastModified: modified, payloadSize: header.PayloadSize}, nil
}

func recordText(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	return string([]rune(value))
}

func recordMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make(map[string]string, len(metadata))
	for _, key := range keys {
		result[recordText(key)] = recordText(metadata[key])
	}
	return result
}
