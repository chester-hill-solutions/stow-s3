package storage

import (
	"strings"
	"time"
)

const httpTimeFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

// CheckWritePreconditions applies the shared object-write preconditions.
func CheckWritePreconditions(opts PutOptions, existing *ObjectMeta) error {
	if opts.IfMatch != "" {
		if existing == nil || !matchesETagHeader(opts.IfMatch, existing.ETag, false) {
			return ErrPreconditionFailed
		}
	}
	if opts.IfNoneMatch != "" && existing != nil && matchesETagHeader(opts.IfNoneMatch, existing.ETag, true) {
		return ErrPreconditionFailed
	}
	return nil
}

// ValidateCopyRequest applies the name validation every copy needs, whichever
// end of it a backend checks first.
//
// It is shared because the four names are four names: a copy with an invalid
// source key and a valid destination is the same request whichever order a store
// looked at them in, and an order that reported a different error for the same
// request would make the store's choice visible to a client.
func ValidateCopyRequest(req CopyRequest) error {
	if err := ValidateBucketName(req.SourceBucket); err != nil {
		return err
	}
	if err := ValidateKey(req.SourceKey); err != nil {
		return err
	}
	if err := ValidateBucketName(req.DestBucket); err != nil {
		return err
	}
	return ValidateKey(req.DestKey)
}

// CheckCopySourceConditions applies the source-side preconditions of a copy.
//
// It is CheckWritePreconditions with the source in the position a put's
// destination occupies, because it is the same question: does the version about
func CheckCopySourceConditions(opts CopyOptions, source *ObjectMeta) error {
	if opts.SourceIfMatch != "" {
		if source == nil || !matchesETagHeader(opts.SourceIfMatch, source.ETag, false) {
			return ErrPreconditionFailed
		}
	} else if err := checkCopyDate(opts.SourceIfUnmodifiedSince, source, false); err != nil {
		return err
	}
	if opts.SourceIfNoneMatch != "" {
		if source != nil && matchesETagHeader(opts.SourceIfNoneMatch, source.ETag, true) {
			return ErrPreconditionFailed
		}
		return nil
	}
	return checkCopyDate(opts.SourceIfModifiedSince, source, true)
}

func checkCopyDate(raw string, source *ObjectMeta, modified bool) error {
	if raw == "" {
		return nil
	}
	when, err := time.Parse(httpTimeFormat, raw)
	if err != nil || source == nil {
		return ErrPreconditionFailed
	}
	if source.LastModified.Truncate(time.Second).After(when) != modified {
		return ErrPreconditionFailed
	}
	return nil
}

func matchesETagHeader(header, actual string, weak bool) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		candidateValue := candidate
		if strings.HasPrefix(strings.ToLower(candidateValue), "w/") {
			if !weak {
				continue
			}
			candidateValue = candidateValue[2:]
		}
		if strings.EqualFold(strings.Trim(candidateValue, "\""), strings.Trim(actual, "\"")) {
			return true
		}
	}
	return false
}
