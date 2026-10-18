package fs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	storage "github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// multipartManifest is the on-disk record of an upload in flight.
//
// The object properties live here, not in the caller's memory, because the
// upload survives the process that started it: a completion may be issued by a
// different run against a manifest written by an earlier one. A manifest that
// recorded only the bucket, key and time left the completed object with no
// content type and no user metadata, because there was nowhere left to read them
// from.
type multipartManifest struct {
	Bucket            string            `json:"bucket"`
	Key               string            `json:"key"`
	Initiated         time.Time         `json:"initiated"`
	ContentType       string            `json:"content_type,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	ChecksumAlgorithm string            `json:"checksum_algorithm,omitempty"`
	ChecksumValue     string            `json:"checksum_value,omitempty"`
}

// upload renders the manifest as the descriptor the storage contract describes.
func (m multipartManifest) upload(uploadID string) storage.MultipartUpload {
	return storage.MultipartUpload{
		UploadID:  uploadID,
		Bucket:    m.Bucket,
		Key:       m.Key,
		Initiated: m.Initiated,
		Options: storage.MultipartOptions{
			ContentType:       m.ContentType,
			Metadata:          storage.CloneMetadata(m.Metadata),
			ChecksumAlgorithm: m.ChecksumAlgorithm,
			ChecksumValue:     m.ChecksumValue,
		},
	}
}

func (s *FilesystemStore) CreateMultipartUpload(_ context.Context, bucket, key string, opts storage.MultipartOptions) (*storage.MultipartUpload, error) {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	if err := storage.ValidateKey(key); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usageRevision++
	if err := s.settleSaveLocked(); err != nil {
		return nil, err
	}

	if _, err := os.Stat(s.bucketDir(bucket)); os.IsNotExist(err) {
		return nil, storage.ErrBucketNotFound
	}

	uploadID, err := storage.NewUploadID()
	if err != nil {
		return nil, err
	}
	dir := s.multipartDir(uploadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	manifest := multipartManifest{
		Bucket:            bucket,
		Key:               key,
		Initiated:         time.Now().UTC(),
		ContentType:       opts.ContentType,
		Metadata:          storage.CloneMetadata(opts.Metadata),
		ChecksumAlgorithm: storage.NormalizeChecksumAlgorithm(opts.ChecksumAlgorithm),
		ChecksumValue:     opts.ChecksumValue,
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err := s.writeCapacityFile(filepath.Join(dir, "manifest.json"), manifestData); err != nil {
		_ = s.removeCapacityPath(dir)
		return nil, err
	}
	upload := manifest.upload(uploadID)
	return &upload, nil
}

func (s *FilesystemStore) GetMultipartUpload(_ context.Context, uploadID string) (*storage.MultipartUpload, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	manifest, err := readMultipartManifest(s.multipartDir(uploadID))
	if err != nil {
		return nil, err
	}
	upload := manifest.upload(uploadID)
	return &upload, nil
}

func (s *FilesystemStore) UploadPart(_ context.Context, uploadID string, partNumber int, body io.Reader) (*storage.PartInfo, error) {
	if partNumber < 1 || partNumber > 10000 {
		return nil, storage.ErrInvalidPart
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usageRevision++
	if err := s.settleSaveLocked(); err != nil {
		return nil, err
	}

	dir := s.multipartDir(uploadID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, storage.ErrUploadNotFound
	}
	etag, data, err := storage.ETagForReader(body)
	if err != nil {
		return nil, err
	}
	partPath := filepath.Join(dir, fmt.Sprintf("part-%05d", partNumber))
	if err := s.writeCapacityFile(partPath, data); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &storage.PartInfo{
		PartNumber:   partNumber,
		ETag:         etag,
		Size:         int64(len(data)),
		LastModified: now,
	}, nil
}

func (s *FilesystemStore) CompleteMultipartUpload(_ context.Context, uploadID string, parts []storage.PartInfo) (*storage.ObjectMeta, error) {
	if err := storage.ValidateMultipartPartNumbers(parts); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usageRevision++
	if err := s.settleSaveLocked(); err != nil {
		return nil, err
	}

	dir := s.multipartDir(uploadID)
	manifest, err := readMultipartManifest(dir)
	if err != nil {
		return nil, err
	}

	// Resolve and check every part before assembling any of it, so a completion
	// that is going to be refused costs no assembly and leaves nothing behind.
	chunks := make([][]byte, 0, len(parts))
	sizes := make([]int64, 0, len(parts))
	partETags := make([]string, 0, len(parts))
	var total int64
	for _, p := range parts {
		partPath := filepath.Join(dir, fmt.Sprintf("part-%05d", p.PartNumber))
		data, err := os.ReadFile(partPath)
		if err != nil {
			return nil, storage.ErrInvalidPart
		}
		storedETag := storage.ETagForBytes(data)
		if p.ETag == "" || !storage.ETagEqual(p.ETag, storedETag) {
			return nil, storage.ErrInvalidPart
		}
		chunks = append(chunks, data)
		sizes = append(sizes, int64(len(data)))
		partETags = append(partETags, storedETag)
		total += int64(len(data))
	}
	if err := storage.ValidateMinPartSizes(sizes); err != nil {
		return nil, err
	}

	upload := manifest.upload(uploadID)
	if err := verifyMultipartChecksum(upload.Options, chunks); err != nil {
		return nil, err
	}

	etag := storage.CompletionETag(partETags)
	recordVersion, err := storage.NewRecordVersion()
	if err != nil {
		return nil, err
	}
	completion := storage.Completion{
		Upload:      upload,
		ETag:        etag,
		Size:        total,
		VersionID:   recordVersion,
		CompletedAt: time.Now().UTC(),
	}
	meta := completion.ObjectMeta()
	record := objectRecord{
		RecordVersion:     recordVersion,
		payloadSize:       total,
		ETag:              etag,
		ContentType:       meta.ContentType,
		Metadata:          meta.Metadata,
		ChecksumAlgorithm: meta.ChecksumAlgorithm,
		ChecksumValue:     meta.ChecksumValue,
		LastModified:      meta.LastModified,
	}
	// A completion publishes the object like any other write, so a held object
	// refuses it before publication.
	if err := s.checkRecoveryHoldsLocked(manifest.Bucket, manifest.Key); err != nil {
		return nil, err
	}
	if err := s.writeObject(manifest.Bucket, manifest.Key, record, chunks...); err != nil {
		return nil, err
	}
	if err := s.removeCapacityPath(dir); err != nil {
		// The object is already committed; retain the upload so callers can retry cleanup.
		return &meta, storage.CommittedError(fmt.Errorf("cleanup multipart upload: %w", err))
	}
	return &meta, nil
}

func (s *FilesystemStore) ValidateMultipartUpload(_ context.Context, uploadID, bucket, key string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	manifest, err := readMultipartManifest(s.multipartDir(uploadID))
	if errors.Is(err, storage.ErrUploadNotFound) {
		return storage.ErrNoSuchUpload
	}
	if err != nil {
		return err
	}
	if manifest.Bucket != bucket || manifest.Key != key {
		return storage.ErrNoSuchUpload
	}
	return nil
}

func (s *FilesystemStore) ListMultipartUploads(_ context.Context, bucket string, opts storage.MultipartListOptions) (*storage.MultipartListResult, error) {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := os.Stat(s.bucketDir(bucket)); os.IsNotExist(err) {
		return nil, storage.ErrBucketNotFound
	}
	entries, err := os.ReadDir(filepath.Join(s.dataDir, ".multipart"))
	if err != nil {
		if os.IsNotExist(err) {
			return storage.PaginateMultipartUploads(nil, opts), nil
		}
		return nil, err
	}
	uploads := make([]storage.MultipartUpload, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifest, err := readMultipartManifest(filepath.Join(s.dataDir, ".multipart", entry.Name()))
		if err != nil {
			return nil, err
		}
		if manifest.Bucket != bucket {
			continue
		}
		uploads = append(uploads, manifest.upload(entry.Name()))
	}
	return storage.PaginateMultipartUploads(uploads, opts), nil
}

func (s *FilesystemStore) AbortMultipartUpload(_ context.Context, uploadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usageRevision++
	if err := s.settleSaveLocked(); err != nil {
		return err
	}

	dir := s.multipartDir(uploadID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return storage.ErrUploadNotFound
	}
	return s.removeCapacityPath(dir)
}

func (s *FilesystemStore) listParts(_ context.Context, uploadID string) ([]storage.PartInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.multipartDir(uploadID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, storage.ErrUploadNotFound
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var parts []storage.PartInfo
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "part-") {
			continue
		}
		var num int
		if _, err := fmt.Sscanf(e.Name(), "part-%05d", &num); err != nil {
			continue
		}
		st, err := e.Info()
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		parts = append(parts, storage.PartInfo{
			PartNumber:   num,
			ETag:         storage.ETagForBytes(data),
			Size:         st.Size(),
			LastModified: st.ModTime().UTC(),
		})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	return parts, nil
}

// ListPartsPage returns a marker-paginated page of uploaded parts.
func (s *FilesystemStore) ListPartsPage(ctx context.Context, uploadID string, opts storage.ListPartsOptions) (*storage.ListPartsResult, error) {
	parts, err := s.listParts(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	return storage.PaginateParts(parts, opts), nil
}

func (s *FilesystemStore) ListParts(ctx context.Context, uploadID string) ([]storage.PartInfo, error) {
	return s.listParts(ctx, uploadID)
}

func readMultipartManifest(dir string) (multipartManifest, error) {
	var m multipartManifest
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		if os.IsNotExist(err) {
			if _, dirErr := os.Stat(dir); os.IsNotExist(dirErr) {
				return m, storage.ErrUploadNotFound
			} else if dirErr != nil {
				return m, dirErr
			}
		}
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if storage.ValidateBucketName(m.Bucket) != nil || storage.ValidateKey(m.Key) != nil {
		return m, fmt.Errorf("invalid multipart manifest bucket or key")
	}
	return m, nil
}

// The validated part buffers are the publication source; checksum them without
// allocating a second concatenated payload.
func verifyMultipartChecksum(opts storage.MultipartOptions, parts [][]byte) error {
	if opts.ChecksumValue == "" {
		return nil
	}
	if storage.NormalizeChecksumAlgorithm(opts.ChecksumAlgorithm) == "" {
		return storage.VerifyChecksum(storage.MultipartPutOptions(opts), nil)
	}
	digest, err := storage.NewChecksumDigest(opts.ChecksumAlgorithm)
	if err != nil {
		return err
	}
	for _, part := range parts {
		_, _ = digest.Write(part)
	}
	if base64.StdEncoding.EncodeToString(digest.Sum(nil)) != opts.ChecksumValue {
		return storage.ErrChecksumMismatch
	}
	return nil
}
