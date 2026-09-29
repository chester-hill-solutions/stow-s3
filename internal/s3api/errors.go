package s3api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

const xmlNS = "http://s3.amazonaws.com/doc/2006-03-01/"

type s3Error struct {
	Code       string
	Message    string
	Resource   string
	StatusCode int
}

func (e s3Error) Error() string {
	return e.Message
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func writeError(w http.ResponseWriter, r *http.Request, err s3Error) {
	reqID := requestIDFromContext(r.Context())
	if reqID == "" {
		reqID = newRequestID()
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-amz-request-id", reqID)
	setCORS(w, r)
	w.WriteHeader(err.StatusCode)
	_ = xml.NewEncoder(w).Encode(errorResponse{
		Code:      err.Code,
		Message:   err.Message,
		Resource:  err.Resource,
		RequestID: reqID,
	})
}

type errorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource"`
	RequestID string   `xml:"RequestId"`
}

// maxMetadataBytes bounds the user metadata a single write may carry, in the
// sum of the names and values. It is the same ceiling for a single write and for
// a multipart initiation, which is why both read it from here.
const maxMetadataBytes = 2048

// DefaultMaxRequestBytes bounds a single request body when Config does not set
// a limit. It matches the documented default agent-session request ceiling.
const DefaultMaxRequestBytes int64 = 8 * 1024 * 1024

// ReadTimeout bounds reading a whole request, and WriteTimeout bounds the
// response. They are generous enough for a large upload on a slow link while
// still preventing a stalled peer from holding a connection and goroutine
// indefinitely.
const (
	ReadTimeout  = 5 * time.Minute
	WriteTimeout = 5 * time.Minute
)

// isRequestTooLarge reports whether err came from the request body limit.
func isRequestTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}

func requestTooLargeError(resource string, limit int64) s3Error {
	return s3Error{
		Code:       "EntityTooLarge",
		Message:    fmt.Sprintf("Your proposed upload exceeds the maximum allowed object size of %d bytes", limit),
		Resource:   resource,
		StatusCode: http.StatusBadRequest,
	}
}

// bodyReadError maps a failure to read the request body. Reading the body is
// where the size limit is enforced, so an over-limit body surfaces here and must
// keep reporting EntityTooLarge rather than a generic read failure.
func bodyReadError(err error, resource string, limit int64) s3Error {
	if isRequestTooLarge(err) {
		var tooLarge *http.MaxBytesError
		_ = errors.As(err, &tooLarge)
		return requestTooLargeError(resource, tooLarge.Limit)
	}
	return s3Error{
		Code:       "InvalidArgument",
		Message:    fmt.Sprintf("could not read request body: %v", err),
		Resource:   resource,
		StatusCode: http.StatusBadRequest,
	}
}

// storageErrorCase maps one sentinel to the S3 error a client should see.
//
// This was a fourteen-case if/else chain, and adding the authority refusal
// pushed it over the complexity limit. A table is the right shape for it anyway:
// the entries differ only in which sentinel they match and what they say, so a
// new mapping is a new row rather than a new branch, and the order — which
// matters, because the first match wins — is visible at a glance.
type storageErrorCase struct {
	sentinel   error
	code       string
	message    string
	statusCode int
}

var storageErrorCases = []storageErrorCase{
	{storage.ErrInvalidMetadata, "InvalidArgument", "Invalid or conflicting user metadata", http.StatusBadRequest},
	{storage.ErrBucketNotFound, "NoSuchBucket", "The specified bucket does not exist", http.StatusNotFound},
	{storage.ErrInvalidBucketName, "InvalidBucketName", "The specified bucket name is not valid", http.StatusBadRequest},
	{storage.ErrObjectNotFound, "NoSuchKey", "The specified key does not exist", http.StatusNotFound},
	{storage.ErrBucketNotEmpty, "BucketNotEmpty", "The bucket you tried to delete is not empty", http.StatusConflict},
	{storage.ErrInvalidKey, "InvalidArgument", "Invalid object key", http.StatusBadRequest},
	{storage.ErrInvalidPart, "InvalidPart", "One or more of the specified parts could not be found", http.StatusBadRequest},
	{storage.ErrEntityTooSmall, "EntityTooSmall", "Your proposed upload is smaller than the minimum allowed object size", http.StatusBadRequest},
	{storage.ErrUploadNotFound, "NoSuchUpload", "The specified multipart upload does not exist", http.StatusNotFound},
	{storage.ErrNoSuchUpload, "NoSuchUpload", "The specified multipart upload does not exist", http.StatusNotFound},
	{storage.ErrPreconditionFailed, "PreconditionFailed", "At least one of the pre-conditions you specified did not hold", http.StatusPreconditionFailed},
	{runtime.ErrQuotaExceeded, "InsufficientStorage", "The runtime storage quota was exceeded", http.StatusInsufficientStorage},
	{runtime.ErrClosed, "ServiceUnavailable", "The runtime is closed", http.StatusServiceUnavailable},
	{runtime.ErrInvalidListLimit, "InvalidArgument", "The list limit is invalid", http.StatusBadRequest},
	{runtime.ErrMultipartUnsupported, "NotImplemented", "Multipart operations are not supported by this runtime profile", http.StatusNotImplemented},
}

func mapStorageError(err error, resource string) s3Error {
	// Three cases have to read the error rather than merely recognise it, so they
	// stay ahead of the table.
	if isRequestTooLarge(err) {
		var tooLarge *http.MaxBytesError
		_ = errors.As(err, &tooLarge)
		return requestTooLargeError(resource, tooLarge.Limit)
	}
	var denied *authority.ErrNotAuthorized
	if errors.As(err, &denied) {
		// A refusal is 403 AccessDenied, which is what S3 returns and what an
		// SDK feature-gate expects. Falling through to InternalError would
		// misreport a permission decision as a server fault and invite a retry
		// of something that can never succeed.
		//
		// The message names the operation and nothing about the environment, so
		// a caller learns what it may not do without learning what it may.
		return s3Error{
			Code:       "AccessDenied",
			Message:    fmt.Sprintf("Access Denied: %s is not permitted by this environment", denied.Operation),
			Resource:   resource,
			StatusCode: http.StatusForbidden,
		}
	}
	// Ahead of the upstream table and the storage table both, because this error
	// carries a cause that one of them will match and the cause is the wrong
	// answer.
	//
	// ErrMutationCommitted says the object was written to the authoritative local
	// store and a later step failed. Its cause is then whatever that step hit —
	// commonly ErrObjectNotFound, because the upstream did not have the bucket —
	// and the storage table maps that to a 404 NoSuchKey. So the server told a
	// caller that the object does not exist while having just written it, and the
	// two statements contradict each other. A caller that believed the 404 and
	// read the key back would find it; a caller that retried would never succeed,
	// because nothing about the next attempt changes.
	//
	// 5xx is the honest class: the local state is committed and the propagation is
	// unfinished, so a retry of the same body is safe and the outbox is still
	// trying. The cause is not repeated in the message, for the same reason the
	// upstream table does not repeat it.
	if errors.Is(err, storage.ErrMutationCommitted) {
		return s3Error{
			Code:       "InternalError",
			Message:    "The write is committed locally but a follow-up step failed; the object is readable and a retry is safe",
			Resource:   resource,
			StatusCode: http.StatusInternalServerError,
		}
	}
	if upstreamErr := upstreamFailure(err, resource); upstreamErr != nil {
		return *upstreamErr
	}
	for _, mapping := range storageErrorCases {
		if errors.Is(err, mapping.sentinel) {
			return s3Error{
				Code:       mapping.code,
				Message:    mapping.message,
				Resource:   resource,
				StatusCode: mapping.statusCode,
			}
		}
	}
	return s3Error{Code: "InternalError", Message: "internal storage error", Resource: resource, StatusCode: http.StatusInternalServerError}
}

// upstreamFailure answers for a request that failed because the upstream did.
//
// Before this existed, a run-through server whose upstream had gone away answered
// every read with InternalError and "internal storage error". That is the one
// situation this product exists to be running when, and the answer was the shape of
// a stow bug: 500, unretryable, and a message naming stow's own storage layer
// rather than the dependency that was not there. An agent reading a log would have
// concluded the workspace was corrupt.
//
// Three cases, and the split is by who is at fault and whether waiting helps:
//
//   - no response at all: 503. The upstream was asked and never answered. Nothing
//     stow did wrong, the condition is usually temporary, and it is the condition
//     the cache exists to absorb.
//   - a 5xx from the upstream: 503 for the same reason. The upstream is the thing
//     that failed, and 500 would blame stow for someone else's outage.
//   - a 4xx from the upstream: passed through. The upstream gave a real answer, and
//     substituting a guess would replace a decision somebody made on purpose.
func upstreamFailure(err error, resource string) *s3Error {
	var upstream *runthrough.UpstreamError
	if !errors.As(err, &upstream) || upstream == nil {
		return nil
	}
	switch {
	case upstream.StatusCode >= 500:
		return &s3Error{
			Code:     "ServiceUnavailable",
			Message:  fmt.Sprintf("The upstream returned %d. This is not a fault in the server you are talking to", upstream.StatusCode),
			Resource: resource, StatusCode: http.StatusServiceUnavailable,
		}
	case upstream.StatusCode >= 400:
		code := upstream.Code
		if code == "" {
			code = "UpstreamError"
		}
		return &s3Error{
			Code:     code,
			Message:  fmt.Sprintf("The upstream refused this request with status %d", upstream.StatusCode),
			Resource: resource, StatusCode: upstream.StatusCode,
		}
	case upstream.StatusCode == 0:
		return &s3Error{
			Code:     "ServiceUnavailable",
			Message:  "The upstream could not be reached. This is a temporary condition and the request is safe to retry",
			Resource: resource, StatusCode: http.StatusServiceUnavailable,
		}
	}
	return nil
}

func writeXML(w http.ResponseWriter, r *http.Request, status int, v any) {
	reqID := requestIDFromContext(r.Context())
	if reqID == "" {
		reqID = newRequestID()
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-amz-request-id", reqID)
	setCORS(w, r)
	w.WriteHeader(status)
	enc := xml.NewEncoder(w)
	if _, ok := v.(struct{ XMLName xml.Name }); ok {
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	}
	_ = enc.Encode(v)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

type ctxKey string

const requestIDKey ctxKey = "requestID"

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func requestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}
