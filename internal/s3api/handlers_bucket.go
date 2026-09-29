package s3api

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// handleGetBucketLocation answers ?location.
//
// It existed as a 400, which is the one status an SDK cannot recover from: the
// operation is part of a bucket's own surface, so a client doing a bucket feature
// probe or configuring a client from an existing bucket hits it before anything
// else works, and "Invalid request" names neither the operation nor the fix.
//
// The answer is the empty string, which is what S3 returns for us-east-1 and what
// every SDK reads as the default region. There is one region here and it is not
// configurable, so a single fixed value is the honest answer rather than a
// fabricated one — and a caller that needs to know which region this is can be told
// by the endpoint it is already talking to.
func (s *Server) handleGetBucketLocation(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket string) {
	if _, err := s.store.HeadBucket(ctx, bucket); err != nil {
		writeError(w, r, mapStorageError(err, "/"+bucket))
		return
	}
	w.Header().Set("x-amz-request-id", requestIDFromContext(ctx))
	writeXML(w, r, http.StatusOK, locationConstraint{})
}

func (s *Server) handleListBuckets(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	buckets, err := s.store.ListBuckets(ctx)
	if err != nil {
		writeError(w, r, mapStorageError(err, "/"))
		return
	}
	items := make([]bucketEntry, 0, len(buckets))
	for _, b := range buckets {
		items = append(items, bucketEntry{Name: b.Name, CreationDate: formatTime(b.CreationDate)})
	}
	writeXML(w, r, http.StatusOK, newListBucketsResult(items))
}

func (s *Server) handleCreateBucket(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket string) {
	if !storage.ValidBucketName(bucket) {
		writeError(w, r, s3Error{Code: "InvalidBucketName", Message: "Invalid bucket name", Resource: "/" + bucket, StatusCode: http.StatusBadRequest})
		return
	}
	err := s.store.CreateBucket(ctx, bucket)
	if err != nil && err != storage.ErrBucketExists {
		writeError(w, r, mapStorageError(err, "/"+bucket))
		return
	}
	w.Header().Set("x-amz-request-id", requestIDFromContext(ctx))
	setCORS(w, r)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleHeadBucket(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket string) {
	_, err := s.store.HeadBucket(ctx, bucket)
	if err != nil {
		writeError(w, r, mapStorageError(err, "/"+bucket))
		return
	}
	w.Header().Set("x-amz-request-id", requestIDFromContext(ctx))
	setCORS(w, r)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteBucket(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket string) {
	err := s.store.DeleteBucket(ctx, bucket)
	if err != nil {
		writeError(w, r, mapStorageError(err, "/"+bucket))
		return
	}
	w.Header().Set("x-amz-request-id", requestIDFromContext(ctx))
	setCORS(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListObjectsV2(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket string, q url.Values) {
	prefix := q.Get("prefix")
	delimiter := q.Get("delimiter")
	continuation := q.Get("continuation-token")
	if continuation != "" && storage.ValidateKey(continuation) != nil {
		writeError(w, r, s3Error{Code: "InvalidArgument", Message: "Invalid continuation token", StatusCode: http.StatusBadRequest})
		return
	}
	startAfter := q.Get("start-after")
	encodingType := q.Get("encoding-type")
	maxKeys := 1000
	if mk := q.Get("max-keys"); mk != "" {
		if n, err := strconv.Atoi(mk); err == nil && n > 0 {
			maxKeys = n
		}
	}

	result, err := s.store.ListObjectsV2(ctx, bucket, storage.ListOptions{
		Prefix:            prefix,
		Delimiter:         delimiter,
		ContinuationToken: continuation,
		MaxKeys:           maxKeys,
		StartAfter:        startAfter,
	})
	if err != nil {
		writeError(w, r, mapStorageError(err, "/"+bucket))
		return
	}

	encodeURL := encodingType == "url"
	respPrefix := prefix
	respDelimiter := delimiter
	respContinuation := result.ContinuationToken
	respNextContinuation := result.NextContinuationToken
	if encodeURL {
		respPrefix = urlEncodeKey(respPrefix)
		respDelimiter = urlEncodeKey(respDelimiter)
		respContinuation = urlEncodeKey(respContinuation)
		respNextContinuation = urlEncodeKey(respNextContinuation)
	}
	resp := listBucketResult{
		Xmlns:                 xmlNS,
		Name:                  bucket,
		Prefix:                respPrefix,
		KeyCount:              result.KeyCount,
		MaxKeys:               maxKeys,
		IsTruncated:           result.IsTruncated,
		ContinuationToken:     respContinuation,
		NextContinuationToken: respNextContinuation,
		Delimiter:             respDelimiter,
	}
	if encodeURL {
		resp.EncodingType = "url"
	}
	for _, o := range result.Objects {
		resp.Contents = append(resp.Contents, objectToEntry(objectMeta{
			Key: o.Key, Size: o.Size, ETag: o.ETag, LastModified: o.LastModified,
		}, encodeURL))
	}
	for _, cp := range result.CommonPrefixes {
		p := cp
		if encodeURL {
			p = urlEncodeKey(cp)
		}
		resp.CommonPrefixes = append(resp.CommonPrefixes, commonPrefix{Prefix: p})
	}
	writeXML(w, r, http.StatusOK, resp)
}

func (s *Server) handlePutObject(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) {
	// Read the body once and share it with every check below. Each of these used
	// to read the whole body itself, which meant a separate full-size copy per
	// check for a single request.
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
	cl := r.ContentLength
	if cl < 0 {
		writeError(w, r, s3Error{Code: "InvalidArgument", Message: "Content-Length required", Resource: resourcePath(bucket, key), StatusCode: http.StatusBadRequest})
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	metadata, err := extractMetadata(r.Header)
	if err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	if len(metadata) > 0 && metadataSize(metadata) > maxMetadataBytes {
		writeError(w, r, s3Error{Code: "InvalidArgument", Message: "Metadata too large", Resource: resourcePath(bucket, key), StatusCode: http.StatusBadRequest})
		return
	}

	meta, err := s.store.PutObject(ctx, bucket, key, newBodyReader(body), storage.PutOptions{
		ContentType:       contentType,
		Metadata:          metadata,
		ChecksumAlgorithm: checksumAlgorithm,
		ChecksumValue:     checksumValue,
		IfMatch:           r.Header.Get("If-Match"),
		IfNoneMatch:       r.Header.Get("If-None-Match"),
	})
	if err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	w.Header().Set("ETag", meta.ETag)
	setChecksumHeader(w, meta)
	writeXML(w, r, http.StatusOK, putObjectResult{ETag: meta.ETag})
}

func (s *Server) handleGetObject(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) {
	rc, meta, err := s.store.GetObject(ctx, bucket, key)
	if err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	defer rc.Close()
	if err := checkReadPreconditions(r.Header, meta); err != nil {
		if errors.Is(err, errNotModified) {
			// Validators only. A 304 has no body, so the representation
			// metadata that describes one must not be sent with it.
			setValidators(w, meta)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}

	rangeHdr := r.Header.Get("Range")
	if rangeHdr != "" {
		s.serveRange(w, r, rc, meta, rangeHdr, bucket, key)
		return
	}

	if err := setObjectHeaders(w, meta); err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	setCORS(w, r)
	w.Header().Set("x-amz-request-id", requestIDFromContext(ctx))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleHeadObject(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) {
	meta, err := s.store.HeadObject(ctx, bucket, key)
	if err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	if err := checkReadPreconditions(r.Header, meta); err != nil {
		if errors.Is(err, errNotModified) {
			// Validators only. A 304 has no body, so the representation
			// metadata that describes one must not be sent with it.
			setValidators(w, meta)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	if err := setObjectHeaders(w, meta); err != nil {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	setCORS(w, r)
	w.Header().Set("x-amz-request-id", requestIDFromContext(ctx))
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteObject(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) {
	err := s.store.DeleteObject(ctx, bucket, key)
	if err != nil && err != storage.ErrObjectNotFound {
		writeError(w, r, mapStorageError(err, resourcePath(bucket, key)))
		return
	}
	w.Header().Set("x-amz-request-id", requestIDFromContext(ctx))
	setCORS(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteObjects(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket string) {
	// DeleteObjects decodes XML straight from the body, so it needs the same
	// single read every other handler does.
	body, bodyErr := requestBody(r)
	if bodyErr != nil {
		writeError(w, r, bodyReadError(bodyErr, "/"+bucket, s.config.MaxRequestBytes))
		return
	}
	if err := enforceContentLength(r, body); err != nil {
		writeError(w, r, s3Error{Code: "InvalidArgument", Message: err.Error(), Resource: "/" + bucket, StatusCode: http.StatusBadRequest})
		return
	}
	var req deleteObjectsRequest
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, s3Error{Code: "MalformedXML", Message: "Malformed XML", Resource: "/" + bucket, StatusCode: http.StatusBadRequest})
		return
	}
	keys := make([]string, 0, len(req.Objects))
	for _, o := range req.Objects {
		keys = append(keys, o.Key)
	}
	deleted, err := s.store.DeleteObjects(ctx, bucket, keys)
	if err != nil {
		writeError(w, r, mapStorageError(err, "/"+bucket))
		return
	}
	resp := deleteResult{}
	if !req.Quiet {
		for _, k := range deleted {
			resp.Deleted = append(resp.Deleted, deletedEntry{Key: k})
		}
	}
	writeXML(w, r, http.StatusOK, resp)
}

// handleCopyObject dispatches a copy to the path its metadata directive names.
// The paths themselves are in copy.go.
func (s *Server) handleCopyObject(ctx context.Context, w http.ResponseWriter, r *http.Request, dstBucket, dstKey, copySource string) {
	srcBucket, srcKey, err := parseCopySource(copySource)
	if err != nil {
		writeError(w, r, s3Error{Code: "InvalidArgument", Message: err.Error(), Resource: resourcePath(dstBucket, dstKey), StatusCode: http.StatusBadRequest})
		return
	}
	copyReq := copyRequest{sourceBucket: srcBucket, sourceKey: srcKey, destBucket: dstBucket, destKey: dstKey}

	directive, err := readCopyDirective(r)
	if err != nil {
		writeError(w, r, s3Error{Code: "InvalidArgument", Message: err.Error(), Resource: copyReq.resource(), StatusCode: http.StatusBadRequest})
		return
	}

	var meta *storage.ObjectMeta
	if directive == directiveReplace {
		meta, err = s.replaceMetadata(ctx, r, copyReq)
	} else {
		meta, err = s.copyVerbatim(ctx, r, copyReq)
	}
	if err != nil {
		writeError(w, r, mapCopyError(err, copyReq.resource()))
		return
	}
	writeXML(w, r, http.StatusOK, copyObjectResult{
		LastModified: formatTime(meta.LastModified),
		ETag:         meta.ETag,
	})
}
