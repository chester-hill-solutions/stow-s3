package runthrough

import (
	"errors"
	"maps"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// callerFacingPropagationError decides what a direct mutation call returns when
// propagation failed deterministically.
//
// The local write is authoritative, so a failure the caller cannot act on is
// recorded on the outbox entry and reported as success: the caller must not be
// told the object is missing when it was in fact written, and the failed
// propagation is visible in the outbox and /_stow/inspect. A conflict is the
// caller's decision to make and is surfaced; a transient failure is retryable
// and is surfaced too. The retry worker deliberately does not use this — it
// reports every failure so an operator can see it.
func callerFacingPropagationError(err error) error {
	if err == nil || IsTransientRetry(err) || errors.Is(err, ErrUpstreamConflict) {
		return err
	}
	return nil
}

func isFirstPendingForKey(entries []OutboxEntry, target OutboxEntry) bool {
	for _, entry := range entries {
		if entry.Bucket != target.Bucket || entry.Key != target.Key || entry.ID == target.ID {
			continue
		}
		if entry.Terminal {
			continue
		}
		if outboxEntryBefore(entry, target) {
			return false
		}
	}
	return true
}

func outboxEntryBefore(left, right OutboxEntry) bool {
	leftSequence, leftOK := outboxSequenceOK(left.ID)
	rightSequence, rightOK := outboxSequenceOK(right.ID)
	if leftOK && rightOK && leftSequence != rightSequence {
		return leftSequence < rightSequence
	}
	if left.CreatedAt.Equal(right.CreatedAt) {
		return left.ID < right.ID
	}
	return left.CreatedAt.Before(right.CreatedAt)
}

func samePropagatedObject(remote, local *storage.ObjectMeta) bool {
	if !storage.ETagEqual(remote.ETag, local.ETag) || effectiveContentType(remote.ContentType) != effectiveContentType(local.ContentType) {
		return false
	}
	wanted, err := storage.NormalizeUserMetadata(local.Metadata)
	if err != nil {
		return false
	}
	actual, err := storage.NormalizeUserMetadata(remote.Metadata)
	if err != nil || !maps.Equal(actual, wanted) {
		return false
	}
	return local.ChecksumValue == "" || remote.ChecksumAlgorithm == local.ChecksumAlgorithm && remote.ChecksumValue == local.ChecksumValue
}

func effectiveContentType(value string) string {
	if value == "" {
		return "application/octet-stream"
	}
	return value
}
