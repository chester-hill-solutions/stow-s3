package fs

import (
	"fmt"
	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	payloadSize       int64
}

func (s *FilesystemStore) writeObject(bucket, key string, record objectRecord, parts ...[]byte) error {
	if err := s.settleSaveLocked(); err != nil {
		return err
	}
	return s.writeObjectRaw(bucket, key, record, parts...)
}

func (s *FilesystemStore) writeObjectRaw(bucket, key string, record objectRecord, parts ...[]byte) error {
	return s.writeObjectBody(bucket, key, record, nil, parts...)
}

func (s *FilesystemStore) writeObjectStream(bucket, key string, record objectRecord, body io.Reader) error {
	return s.writeObjectBody(bucket, key, record, body)
}

func (s *FilesystemStore) writeObjectBody(bucket, key string, record objectRecord, body io.Reader, parts ...[]byte) error {
	path := s.objectPath(bucket, key)
	if s.isBoundedObjectPath(bucket, path) {
		if existing, err := readObjectHeader(path); err == nil && existing.Key != key {
			return fmt.Errorf("object path digest collision: refusing to replace a different key")
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		record.Key = key
	} else {
		record.Key = ""
	}
	if err := s.writeRecordBody(path, record, body, parts...); err != nil {
		return err
	}
	if s.SupportsGuardedWrites() {
		return s.fenceObjectDirectory(filepath.Dir(path))
	}
	return nil
}

func (s *FilesystemStore) writeRecordBody(path string, record objectRecord, body io.Reader, parts ...[]byte) error {
	if len(parts) > 0 {
		header, err := encodeRecordPrefix(record)
		if err != nil {
			return err
		}
		return s.writeCapacityFile(path, append([][]byte{header}, parts...)...)
	}
	if body == nil {
		return s.writeRecord(path, record)
	}
	if s.namespace != nil {
		data, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		record.Data = data
		return s.writeRecord(path, record)
	}
	header, err := encodeRecordPrefix(record)
	if err != nil {
		return err
	}
	return atomicfile.WriteStream(path, 0o644, header, body)
}

func (s *FilesystemStore) writeRecord(path string, record objectRecord) error {
	if s.saves.pending != nil || s.namespace == nil || s.capacityActive {
		return writeObjectRecord(path, record)
	}
	data, err := encodeObjectRecord(record)
	if err != nil {
		return err
	}
	return s.writeCapacityFile(path, data)
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
	header, err := encodeRecordPrefix(record)
	if err != nil {
		return err
	}
	return atomicfile.WriteParts(path, 0o644, header, record.Data)
}

func (s *FilesystemStore) readObjectHeader(bucket, key string) (objectRecord, error) {
	record, err := readObjectHeader(s.objectPath(bucket, key))
	if err == nil && record.Key != "" && record.Key != key {
		err = fmt.Errorf("object path digest collision: stored key does not match requested key")
	}
	return record, err
}

func (r objectRecord) meta(bucket, key string) storage.ObjectMeta {
	size := r.payloadSize
	if r.Data != nil {
		size = int64(len(r.Data))
	}
	versionID := r.RecordVersion
	if versionID == "" {
		versionID = r.ETag
	}
	return storage.ObjectMeta{
		Bucket:            bucket,
		Key:               key,
		VersionID:         versionID,
		Size:              size,
		ETag:              r.ETag,
		ContentType:       r.ContentType,
		LastModified:      r.LastModified.UTC(),
		Metadata:          storage.CloneMetadata(r.Metadata),
		ChecksumAlgorithm: r.ChecksumAlgorithm,
		ChecksumValue:     r.ChecksumValue,
	}
}

func (s *FilesystemStore) fenceObjectDirectory(dir string) error {
	if _, ok := s.fencedDirs[dir]; ok {
		return nil
	}
	if err := syncSaveAncestors(dir); err != nil {
		return err
	}
	s.fencedDirs[dir] = struct{}{}
	return nil
}

func (s *FilesystemStore) forgetFencedDirectories(root string) {
	for dir := range s.fencedDirs {
		rel, err := filepath.Rel(root, dir)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			delete(s.fencedDirs, dir)
		}
	}
}
