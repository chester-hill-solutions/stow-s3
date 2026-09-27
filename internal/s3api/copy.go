package s3api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// The copy half of the object surface.
//
// It is its own file because a copy is the one object operation whose
// correctness is a question about *when* something happened rather than about
// what it contains, and the answer has to be the same in both of the paths a
// copy can take: the bytes copied and the version the caller's conditions were
// checked against must be the same version by construction.

// copyRequest is one copy: where the bytes come from, where they go, and what
// the request says about the source version.
//
// It is a struct because a copy is naturally six things and because the two
// halves must not be confused: a signature taking two adjacent bucket/key string
// pairs invites swapping them, and a swap still typechecks.
type copyRequest struct {
	sourceBucket string
	sourceKey    string
	destBucket   string
	destKey      string
}

// resource is what an error about this copy names.
func (c copyRequest) resource() string { return resourcePath(c.destBucket, c.destKey) }

type copyDirective int

const (
	// directiveCopy keeps the source's metadata, which is S3's default.
	directiveCopy copyDirective = iota
	// directiveReplace publishes the source's bytes under the request's metadata.
	directiveReplace
)

// errMetadataTooLarge is the refusal for user metadata over the ceiling. It is a
// fresh error rather than a reused sentinel so that it can carry the message S3
// uses, and so nothing that matches on a storage sentinel picks it up by
// accident.
var errMetadataTooLarge = errors.New("metadata too large")

// readCopyDirective reads x-amz-metadata-directive. An unrecognised value is
// refused rather than treated as COPY, which is what S3 does: silently keeping
// the source's metadata for a request that asked for something else would
// succeed at the wrong thing.
func readCopyDirective(r *http.Request) (copyDirective, error) {
	switch raw := strings.ToUpper(strings.TrimSpace(r.Header.Get("x-amz-metadata-directive"))); raw {
	case "", "COPY":
		return directiveCopy, nil
	case "REPLACE":
		return directiveReplace, nil
	default:
		return directiveCopy, errors.New("invalid metadata directive")
	}
}

func mapCopyError(err error, resource string) s3Error {
	if errors.Is(err, errMetadataTooLarge) {
		return s3Error{Code: "InvalidArgument", Message: err.Error(), Resource: resource, StatusCode: http.StatusBadRequest}
	}
	return mapStorageError(err, resource)
}

// copyVerbatim publishes the destination from one version of the source, with
// the source's own metadata.
//
// When the request carries source conditions, they are handed to the store so
// that the version it copies is the version they were checked against. Checking
// them here first would leave a window in which the source changes, and the copy
// would then publish a version the caller never asked about and never saw — the
// one outcome a conditional copy exists to prevent.
//
// A store that cannot evaluate conditions still produces a coherent copy, and the
// conditions are still enforced against a version this request observed. That is
// strictly weaker, and it is what storage.ConditionalCopyStore documents.
func (s *Server) copyVerbatim(ctx context.Context, r *http.Request, req copyRequest) (*storage.ObjectMeta, error) {
	options := copySourceConditions(r)
	if conditional, ok := s.store.(storage.ConditionalCopyStore); ok && hasConditions(options) {
		return conditional.CopyObjectCond(ctx, storage.CopyRequest{
			SourceBucket: req.sourceBucket,
			SourceKey:    req.sourceKey,
			DestBucket:   req.destBucket,
			DestKey:      req.destKey,
			Options:      options,
		})
	}
	// A copy by the unmodified source is refused when the source is gone, which
	// this reports: the source is what was being copied.
	sourceMeta, err := s.store.HeadObject(ctx, req.sourceBucket, req.sourceKey)
	if err != nil {
		return nil, err
	}
	if err := checkCopyPreconditions(r.Header, sourceMeta); err != nil {
		return nil, err
	}
	return s.store.CopyObject(ctx, req.sourceBucket, req.sourceKey, req.destBucket, req.destKey)
}

// replaceMetadata publishes the source's bytes under the request's own metadata.
//
// The bytes still come from a single read, and the source conditions are checked
// against the metadata of that same read rather than against a head taken before
// it. A head-then-read ordering would let a source that moves on in between turn
// a refusal into a copy of a version the caller did not ask for.
func (s *Server) replaceMetadata(ctx context.Context, r *http.Request, req copyRequest) (*storage.ObjectMeta, error) {
	metadata := extractMetadata(r.Header)
	if len(metadata) > 0 && metadataSize(metadata) > maxMetadataBytes {
		return nil, errMetadataTooLarge
	}
	body, sourceMeta, err := s.readSource(ctx, r, req)
	if err != nil {
		return nil, err
	}
	return s.store.PutObject(ctx, req.destBucket, req.destKey, newBodyReader(body), storage.PutOptions{
		ContentType:       copyContentType(r),
		Metadata:          metadata,
		ChecksumAlgorithm: sourceMeta.ChecksumAlgorithm,
		ChecksumValue:     sourceMeta.ChecksumValue,
	})
}

// readSource reads the source's bytes and its metadata together, then checks the
// request's conditions against that metadata.
func (s *Server) readSource(ctx context.Context, r *http.Request, req copyRequest) ([]byte, *storage.ObjectMeta, error) {
	rc, sourceMeta, err := s.store.GetObject(ctx, req.sourceBucket, req.sourceKey)
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()
	if err := checkCopyPreconditions(r.Header, sourceMeta); err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, err
	}
	return body, sourceMeta, nil
}

// copyContentType is the content type a REPLACE copy publishes under, defaulting
// the way a plain write does.
func copyContentType(r *http.Request) string {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

// copySourceConditions reads the copy-source preconditions in the form the store
// takes. hasConditions distinguishes "no conditions" from "conditions that happen
// to be empty", because only the first warrants the unconditional copy path.
func copySourceConditions(r *http.Request) storage.CopyOptions {
	return storage.CopyOptions{
		SourceIfMatch:     r.Header.Get("x-amz-copy-source-if-match"),
		SourceIfNoneMatch: r.Header.Get("x-amz-copy-source-if-none-match"),
	}
}

func hasConditions(options storage.CopyOptions) bool {
	return options.SourceIfMatch != "" || options.SourceIfNoneMatch != ""
}
