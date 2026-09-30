package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
)

const objectRecordVersion = 1

type objectRecord struct {
	Version       int    `json:"version"`
	RecordVersion string `json:"record_version,omitempty"`
	SaveRequest   string `json:"save_request,omitempty"`
	SaveMeaning   string `json:"save_meaning,omitempty"`
	// Key is set for digest-addressed records, whose pathname cannot be
	// reversed into the original key. Legacy flat and sharded records omit it.
	Key               string            `json:"key,omitempty"`
	Data              []byte            `json:"data"`
	ContentType       string            `json:"content_type,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	ETag              string            `json:"etag"`
	ChecksumAlgorithm string            `json:"checksum_algorithm,omitempty"`
	ChecksumValue     string            `json:"checksum_value,omitempty"`
	LastModified      time.Time         `json:"last_modified"`
}

func (s *FilesystemStore) writeObject(bucket, key string, record objectRecord) error {
	if err := s.settleSaveLocked(); err != nil {
		return err
	}
	return s.writeObjectRaw(bucket, key, record)
}

func (s *FilesystemStore) writeObjectRaw(bucket, key string, record objectRecord) error {
	path := s.objectPath(bucket, key)
	if s.isBoundedObjectPath(bucket, path) {
		if existing, err := readObjectRecord(path); err == nil && existing.Key != key {
			return fmt.Errorf("object path digest collision: refusing to replace a different key")
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		record.Key = key
	} else {
		record.Key = ""
	}
	if err := writeObjectRecord(path, record); err != nil {
		return err
	}
	if s.SupportsGuardedWrites() {
		return syncSaveAncestors(filepath.Dir(path))
	}
	return nil
}

func (s *FilesystemStore) readObject(bucket, key string) (objectRecord, error) {
	record, err := readObjectRecord(s.objectPath(bucket, key))
	if err != nil {
		return record, err
	}
	if record.Key != "" && record.Key != key {
		return objectRecord{}, fmt.Errorf("object path digest collision: stored key does not match requested key")
	}
	return record, nil
}

func writeObjectRecord(path string, record objectRecord) error {
	record.Version = objectRecordVersion
	return writeJSONAtomic(path, record)
}

func readObjectRecord(path string) (objectRecord, error) {
	var record objectRecord
	data, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, fmt.Errorf("decode object record: %w", err)
	}
	if record.Version != objectRecordVersion {
		return record, fmt.Errorf("unsupported object record version %d", record.Version)
	}
	if record.Data == nil {
		record.Data = []byte{}
	}
	return record, nil
}

func (r objectRecord) meta(bucket, key string) storage.ObjectMeta {
	versionID := r.RecordVersion
	if versionID == "" {
		versionID = r.ETag
	}
	return storage.ObjectMeta{
		Bucket:            bucket,
		Key:               key,
		VersionID:         versionID,
		Size:              int64(len(r.Data)),
		ETag:              r.ETag,
		ContentType:       r.ContentType,
		LastModified:      r.LastModified.UTC(),
		Metadata:          storage.CloneMetadata(r.Metadata),
		ChecksumAlgorithm: r.ChecksumAlgorithm,
		ChecksumValue:     r.ChecksumValue,
	}
}
