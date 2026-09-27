package storage

// The memory backend's multipart completion: resolve, validate, then assemble
// once. Separate from the rest of the store because the order of those steps is
// the whole of its correctness, and it is the step that materializes the object.

import (
	"context"
	"time"
)

func (s *MemoryStore) CompleteMultipartUpload(_ context.Context, uploadID string, parts []PartInfo) (*ObjectMeta, error) {
	if err := ValidateMultipartPartNumbers(parts); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	mp, bucket, ok := s.findMultipart(uploadID)
	if !ok {
		return nil, ErrUploadNotFound
	}

	// Every part is resolved and checked before anything is assembled. A
	// completion either publishes all of the object or changes nothing, and it
	// cannot be allowed to discover a bad part after it has spent the memory
	// building the bytes.
	selected := make([]memPart, 0, len(parts))
	sizes := make([]int64, 0, len(parts))
	partETags := make([]string, 0, len(parts))
	var total int64
	for _, p := range parts {
		part, ok := mp.parts[p.PartNumber]
		if !ok {
			return nil, ErrInvalidPart
		}
		if p.ETag == "" || !ETagEqual(p.ETag, part.info.ETag) {
			return nil, ErrInvalidPart
		}
		selected = append(selected, part)
		sizes = append(sizes, part.info.Size)
		partETags = append(partETags, part.info.ETag)
		total += part.info.Size
	}
	if err := ValidateMinPartSizes(sizes); err != nil {
		return nil, err
	}

	// Allocated once, at the exact size.
	//
	// Appending part by part reallocates as it goes, and every reallocation
	// briefly holds the old buffer and the new one at once — so assembling a
	// body of N bytes cost up to 2N on top of the parts already retained, and
	// the retained parts are the amplification this backend is worst at. The
	// total is known the moment the parts are validated, so there is nothing to
	// discover by growing.
	combined := make([]byte, 0, total)
	for _, part := range selected {
		combined = append(combined, part.data...)
	}
	if err := VerifyChecksum(MultipartPutOptions(mp.upload.Options), combined); err != nil {
		return nil, err
	}

	etag := CompletionETag(partETags)
	versionID, err := NewRecordVersion()
	if err != nil {
		return nil, err
	}
	meta := Completion{
		Upload:      mp.upload,
		ETag:        etag,
		Size:        int64(len(combined)),
		VersionID:   versionID,
		CompletedAt: time.Now().UTC(),
	}.ObjectMeta()
	bucket.objects[mp.upload.Key] = &memObject{data: combined, meta: meta}
	delete(bucket.multipart, uploadID)
	out := cloneObjectMeta(meta)
	return &out, nil
}
