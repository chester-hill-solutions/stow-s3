package runtime

import "github.com/chester-hill-solutions/stow-s3/internal/storage"

func (s *usageState) admitCompletion(uploadID string, parts []storage.PartInfo, oldSize int64, exists bool) error {
	upload := s.multipart[uploadID]
	target := objectTarget(upload.upload.Bucket, upload.upload.Key)
	if !exists && !s.objectQuotaFits(target, 1) {
		return ErrQuotaExceeded
	}
	var completedBytes int64
	for _, part := range parts {
		completedBytes += upload.parts[part.PartNumber]
	}
	if upload.bytes() > s.reservedBytes || s.usage.Bytes-oldSize+s.reservedBytes+completedBytes > s.limits.MaxBytes {
		return ErrQuotaExceeded
	}
	return nil
}

func (s *usageState) finishUpload(upload *storage.MultipartUpload, exists bool, err error) bool {
	if !s.recordMutation(err) {
		return false
	}
	clone := storage.CloneMultipartUpload(*upload)
	s.multipart[upload.UploadID] = multipartUsage{upload: clone, parts: make(map[int]int64)}
	target := objectTarget(upload.Bucket, upload.Key)
	s.addMultipartTarget(target)
	if !exists && !s.hasTargetReservation(target) {
		s.reserveTarget(target)
	}
	return true
}

func (s *usageState) finishPart(uploadID string, part *storage.PartInfo, err error) bool {
	if !s.recordMutation(err) {
		return false
	}
	upload := s.multipart[uploadID]
	oldSize := upload.parts[part.PartNumber]
	upload.parts[part.PartNumber] = part.Size
	s.multipart[uploadID] = upload
	s.reservedBytes += part.Size - oldSize
	return true
}

func (s *usageState) removeUpload(uploadID string, exists bool) {
	upload := s.multipart[uploadID]
	s.reservedBytes -= upload.bytes()
	target := objectTarget(upload.upload.Bucket, upload.upload.Key)
	s.removeMultipartTarget(target)
	s.reconcileTargetReservation(target, exists)
	delete(s.multipart, uploadID)
}

func (s *usageState) finishCompletion(uploadID string, oldSize int64, meta *storage.ObjectMeta, exists bool, err error) bool {
	upload := s.multipart[uploadID]
	if !s.finishStoredPut(objectTarget(upload.upload.Bucket, upload.upload.Key), oldSize, meta, exists, err) {
		return false
	}
	if err == nil {
		s.removeUpload(uploadID, true)
	}
	return true
}

func (s *usageState) finishAbort(uploadID string, exists bool, err error) bool {
	if !s.recordMutation(err) {
		return false
	}
	s.removeUpload(uploadID, exists)
	return true
}
