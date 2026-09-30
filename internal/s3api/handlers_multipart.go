package s3api

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *Server) validateMultipartRoute(ctx context.Context, w http.ResponseWriter, r *http.Request, route routeInfo, uploadID string) bool {
	if err := s.multipart.ValidateMultipartUpload(ctx, uploadID, route.bucket, route.key); err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(route.bucket, route.key)))
		return false
	}
	return true
}

// initiationOptions reads the object properties an upload fixes at initiation.
//
// These are the same properties handlePutObject reads, from the same headers, and
// they are passed to the store rather than dropped: a completion cannot know them
// later, because nothing else records them.
func initiationOptions(r *http.Request) (storage.MultipartOptions, *s3Error) {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	metadata, err := extractMetadata(r.Header)
	if err != nil {
		bad := mapStorageError(err, "")
		return storage.MultipartOptions{}, &bad
	}
	if metadataSize(metadata) > maxMetadataBytes {
		return storage.MultipartOptions{}, &s3Error{
			Code:       "InvalidArgument",
			Message:    "Metadata too large",
			StatusCode: http.StatusBadRequest,
		}
	}
	algorithm, value, err := checksumHeaderValues(r)
	if err != nil {
		return storage.MultipartOptions{}, &s3Error{Code: "InvalidArgument", Message: err.Error(), StatusCode: http.StatusBadRequest}
	}
	if algorithm == "CRC64NVME" {
		return storage.MultipartOptions{}, &s3Error{Code: "NotImplemented", Message: "CRC64NVME multipart full-object checksums are not supported", StatusCode: http.StatusNotImplemented}
	}
	return storage.MultipartOptions{
		ContentType:       contentType,
		Metadata:          metadata,
		ChecksumAlgorithm: algorithm,
		ChecksumValue:     value,
	}, nil
}

func (s *Server) handleCreateMultipartUpload(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) {
	opts, badRequest := initiationOptions(r)
	if badRequest != nil {
		badRequest.Resource = resourcePath(bucket, key)
		writeError(w, r, *badRequest)
		return
	}
	upload, err := s.multipart.CreateMultipartUpload(ctx, bucket, key, opts)
	if err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	writeXML(w, r, http.StatusOK, initiateMultipartUploadResult{
		Bucket:   bucket,
		Key:      key,
		UploadID: upload.UploadID,
	})
}

func (s *Server) handleUploadPart(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string, q url.Values) {
	partNum, err := strconv.Atoi(q.Get("partNumber"))
	if err != nil || partNum < 1 || partNum > 10000 {
		writeError(w, r, s3Error{Code: "InvalidArgument", Message: "Invalid part number", Resource: resourcePath(bucket, key), StatusCode: http.StatusBadRequest})
		return
	}
	uploadID := q.Get("uploadId")
	if !s.validateMultipartRoute(ctx, w, r, routeInfo{bucket: bucket, key: key}, uploadID) {
		return
	}

	// Read the body once and share it with every check below, for the same
	// reason as PutObject: each check making its own full-size copy is what
	// amplifies memory per request.
	body, bodyErr := requestBody(r)
	if bodyErr != nil {
		writeError(w, r, bodyReadError(bodyErr, resourcePath(bucket, key), s.config.MaxRequestBytes))
		return
	}
	if err := enforceContentLength(r, body); err != nil {
		writeError(w, r, s3Error{Code: "InvalidArgument", Message: err.Error(), Resource: resourcePath(bucket, key), StatusCode: http.StatusBadRequest})
		return
	}
	if err := verifyContentMD5(r, body); err != nil {
		code := "InvalidArgument"
		if errors.Is(err, storage.ErrMD5Mismatch) {
			code = "BadDigest"
		}
		writeError(w, r, s3Error{Code: code, Message: err.Error(), Resource: resourcePath(bucket, key), StatusCode: http.StatusBadRequest})
		return
	}
	checksumAlgorithm, checksumValue, err := checksumFromRequest(r, body)
	if err != nil {
		code := "InvalidArgument"
		if errors.Is(err, storage.ErrChecksumMismatch) {
			code = "BadDigest"
		}
		writeError(w, r, s3Error{Code: code, Message: err.Error(), Resource: resourcePath(bucket, key), StatusCode: http.StatusBadRequest})
		return
	}
	part, err := s.multipart.UploadPart(ctx, uploadID, partNum, newBodyReader(body))
	if err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	w.Header().Set("ETag", part.ETag)
	if checksumAlgorithm != "" {
		w.Header().Set("x-amz-checksum-"+strings.ToLower(checksumAlgorithm), checksumValue)
	}
	writeXML(w, r, http.StatusOK, uploadPartResult{ETag: part.ETag})
}

func (s *Server) handleCompleteMultipartUpload(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key, uploadID string) {
	if r.Header.Get("x-amz-checksum-crc64nvme") != "" || r.Header.Get("x-amz-checksum-type") != "" {
		writeError(w, r, s3Error{Code: "NotImplemented", Message: "Multipart completion checksum types are not supported", Resource: resourcePath(bucket, key), StatusCode: http.StatusNotImplemented})
		return
	}
	if !s.validateMultipartRoute(ctx, w, r, routeInfo{bucket: bucket, key: key}, uploadID) {
		return
	}
	var req completeMultipartUploadRequest
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, s3Error{Code: "MalformedXML", Message: "Malformed XML", Resource: resourcePath(bucket, key), StatusCode: http.StatusBadRequest})
		return
	}
	parts := make([]storage.PartInfo, 0, len(req.Parts))
	for _, p := range req.Parts {
		etag := strings.Trim(p.ETag, "\"")
		parts = append(parts, storage.PartInfo{PartNumber: p.PartNumber, ETag: "\"" + etag + "\""})
	}

	// The minimum part size is the store's rule, not this layer's, and the store
	// validates the stored sizes of the exact parts being completed before it
	// assembles anything. Asking the store what its parts were and trusting the
	// answer is not enough: a part missing from the listing is skipped rather than
	// refused, so the one case that must never pass is the one the check is blind
	// to.
	//
	// ErrEntityTooSmall is what it reports, which mapStorageError renders as S3's
	// EntityTooSmall. A client gets the same answer whichever store is underneath,
	// and a store used directly enforces the same rule instead of accepting what
	// the S3 layer would have refused.
	meta, err := s.multipart.CompleteMultipartUpload(ctx, uploadID, parts)
	if err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	writeXML(w, r, http.StatusOK, completeMultipartUploadResult{
		Bucket:   bucket,
		Key:      key,
		ETag:     meta.ETag,
		Location: "/" + bucket + "/" + key,
	})
}

func (s *Server) handleAbortMultipartUpload(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key, uploadID string) {
	if !s.validateMultipartRoute(ctx, w, r, routeInfo{bucket: bucket, key: key}, uploadID) {
		return
	}
	err := s.multipart.AbortMultipartUpload(ctx, uploadID)
	if err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	w.Header().Set("x-amz-request-id", requestIDFromContext(ctx))
	setCORS(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMultipartUploads(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket string, q url.Values) {
	maxUploads := 1000
	if raw := q.Get("max-uploads"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			maxUploads = value
		}
	}
	result, err := s.multipart.ListMultipartUploads(ctx, bucket, storage.MultipartListOptions{
		Prefix:         q.Get("prefix"),
		Delimiter:      q.Get("delimiter"),
		KeyMarker:      q.Get("key-marker"),
		UploadIDMarker: q.Get("upload-id-marker"),
		MaxUploads:     maxUploads,
	})
	if err != nil {
		writeError(w, r, mapStorageError(err, "/"+bucket))
		return
	}
	uploads := make([]multipartUploadXML, 0, len(result.Uploads))
	for _, upload := range result.Uploads {
		uploads = append(uploads, multipartUploadXML{
			Key:       upload.Key,
			UploadID:  upload.UploadID,
			Initiated: formatTime(upload.Initiated),
		})
	}
	writeXML(w, r, http.StatusOK, listMultipartUploadsResult{
		Xmlns:              xmlNS,
		Bucket:             bucket,
		KeyMarker:          result.KeyMarker,
		UploadIDMarker:     result.UploadIDMarker,
		NextKeyMarker:      result.NextKeyMarker,
		NextUploadIDMarker: result.NextUploadIDMarker,
		Prefix:             result.Prefix,
		Delimiter:          result.Delimiter,
		MaxUploads:         result.MaxUploads,
		IsTruncated:        result.IsTruncated,
		Uploads:            uploads,
	})
}

func (s *Server) handleListParts(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key, uploadID string) {
	if !s.validateMultipartRoute(ctx, w, r, routeInfo{bucket: bucket, key: key}, uploadID) {
		return
	}
	parts, err := s.multipart.ListParts(ctx, uploadID)
	if err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	entries := make([]partEntry, 0, len(parts))
	for _, p := range parts {
		entries = append(entries, partEntry{
			PartNumber:   p.PartNumber,
			LastModified: formatTime(p.LastModified),
			ETag:         p.ETag,
			Size:         p.Size,
		})
	}
	writeXML(w, r, http.StatusOK, listPartsResult{
		Bucket:   bucket,
		Key:      key,
		UploadID: uploadID,
		MaxParts: 1000,
		Parts:    entries,
	})
}

func (s *Server) serveRange(w http.ResponseWriter, r *http.Request, rc io.ReadCloser, meta *storage.ObjectMeta, rangeHdr, bucket, key string) {
	defer rc.Close()
	start, end, err := parseRange(rangeHdr, meta.Size)
	if err != nil {
		w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(meta.Size, 10))
		writeError(w, r, s3Error{Code: "InvalidRange", Message: "The requested range is not satisfiable", Resource: resourcePath(bucket, key), StatusCode: http.StatusRequestedRangeNotSatisfiable})
		return
	}
	length := end - start + 1

	if seeker, ok := rc.(io.ReadSeeker); ok {
		_, _ = seeker.Seek(start, io.SeekStart)
	} else {
		_, _ = io.CopyN(io.Discard, rc, start)
	}

	// Representation headers but not the whole-object checksum. A 206 carries a
	// fragment, so an x-amz-checksum-* header beside it describes bytes the
	// client never receives, and @aws-sdk/client-s3 hashes the fragment and
	// refuses the response. Verified through that SDK, not by reading it: the
	// shared corpus case range-partial-object fails on the checksum header and
	// passes without it. Content-Length is corrected to the range length below.
	if err := setRepresentationHeaders(w, meta); err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(meta.Size, 10))
	setCORS(w, r)
	w.Header().Set("x-amz-request-id", requestIDFromContext(r.Context()))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = io.CopyN(w, rc, length)
}

func parseRange(hdr string, size int64) (start, end int64, err error) {
	if !strings.HasPrefix(hdr, "bytes=") {
		return 0, 0, errInvalidRange
	}
	spec := strings.TrimPrefix(hdr, "bytes=")
	if strings.HasPrefix(spec, "-") {
		// suffix range: last N bytes
		n, err := strconv.ParseInt(spec[1:], 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, errInvalidRange
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, nil
	}
	parts := strings.SplitN(spec, "-", 2)
	s, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, errInvalidRange
	}
	var e int64
	if parts[1] == "" {
		e = size - 1
	} else {
		e, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, errInvalidRange
		}
	}
	if s < 0 || s >= size || e < s {
		return 0, 0, errInvalidRange
	}
	// An end past the last byte is clamped, not refused. A recipient must treat an
	// unsatisfiable end as the last byte, and clients that ask for "the rest of
	// this" routinely name an offset they inferred rather than measured. Only a
	// range that *begins* past the end is unsatisfiable, which the check above
	// rejects.
	if e >= size {
		e = size - 1
	}
	return s, e, nil
}

var errInvalidRange = &rangeError{}

type rangeError struct{}

func (e *rangeError) Error() string { return "invalid range" }
