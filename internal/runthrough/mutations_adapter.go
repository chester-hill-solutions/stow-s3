package runthrough

import (
	"context"
	"errors"
	"io"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (a *Adapter) DeleteObject(ctx context.Context, bucket, key string) error {
	action := a.decideUpstreamWrite(bucket)
	if action == writeError {
		return ErrLiveWritesDisabled
	}
	if err := a.requireDurableOutbox(action); err != nil {
		return err
	}
	var prepared OutboxEntry
	version := ""
	if action == writePropagate {
		unlock := a.outboxLocks.lock(outboxIdentity(bucket, key))
		defer unlock()
		if err := a.rejectPreparedKey(bucket, key); err != nil {
			return err
		}
		var err error
		version, err = a.localVersionStrict(ctx, bucket, key)
		if err != nil {
			return err
		}
		prepared, err = a.enqueuePreparedIntentLocked(OutboxDelete, bucket, key, version)
		if err != nil {
			return err
		}
	}
	localErr := a.local.DeleteObject(ctx, bucket, key)
	return a.finishDelete(ctx, bucket, key, deleteIntent{entry: prepared, version: version}, localErr)
}

type deleteIntent struct {
	entry   OutboxEntry
	version string
}

func (a *Adapter) finishDelete(ctx context.Context, bucket, key string, intent deleteIntent, localErr error) error {
	prepared, version := intent.entry, intent.version
	if localErr != nil {
		ready := false
		var recoveryErr error
		if prepared.ID != "" {
			ready, recoveryErr = a.reconcilePreparedEntryLocked(ctx, prepared)
		}
		if recoveryErr != nil {
			return errors.Join(localErr, recoveryErr)
		}
		if errors.Is(localErr, storage.ErrObjectNotFound) {
			if ready {
				return storage.CommittedError(a.completeIntentLocked(ctx, prepared))
			}
			return nil
		}
		if ready {
			return storage.CommittedError(localErr)
		}
		return localErr
	}
	a.invalidateCache(ctx, bucket, key)
	if prepared.ID == "" {
		return nil
	}
	if _, err := a.commitPreparedIntent(prepared, version); err != nil {
		return storage.CommittedError(err)
	}
	return storage.CommittedError(a.completeIntentLocked(ctx, prepared))
}

func (a *Adapter) DeleteObjects(ctx context.Context, bucket string, keys []string) ([]string, error) {
	action := a.decideUpstreamWrite(bucket)
	if action == writeError {
		return nil, ErrLiveWritesDisabled
	}
	if err := a.requireDurableOutbox(action); err != nil {
		return nil, err
	}
	var prepared []OutboxEntry
	if action == writePropagate {
		identities := make([]string, 0, len(keys))
		seen := make(map[string]struct{}, len(keys))
		for _, key := range keys {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			identities = append(identities, outboxIdentity(bucket, key))
		}
		unlock := a.outboxLocks.lockMany(identities)
		defer unlock()
		for _, key := range keys {
			if err := a.rejectPreparedKey(bucket, key); err != nil {
				return nil, err
			}
		}
		var err error
		prepared, err = a.prepareDeleteIntents(ctx, bucket, keys)
		if err != nil {
			return nil, err
		}
	}

	deleted, localErr := a.local.DeleteObjects(ctx, bucket, keys)
	if a.separateCache {
		for _, key := range deleted {
			a.invalidateCache(ctx, bucket, key)
		}
	}
	if action != writePropagate {
		return deleted, localErr
	}
	if localErr != nil {
		return deleted, a.reconcilePreparedEntriesAfterError(ctx, prepared, localErr)
	}

	entries, err := a.commitDeleteIntents(prepared, deleted)
	if err != nil {
		return deleted, err
	}
	return deleted, a.propagateIntents(ctx, entries)
}

func (a *Adapter) prepareDeleteIntents(ctx context.Context, bucket string, keys []string) ([]OutboxEntry, error) {
	entries := make([]OutboxEntry, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		previousVersion, err := a.localVersionStrict(ctx, bucket, key)
		if err != nil {
			return nil, err
		}
		entry, err := a.enqueuePreparedIntentLocked(OutboxDelete, bucket, key, previousVersion)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (a *Adapter) commitDeleteIntents(prepared []OutboxEntry, deleted []string) ([]OutboxEntry, error) {
	deletedSet := make(map[string]struct{}, len(deleted))
	for _, key := range deleted {
		deletedSet[key] = struct{}{}
	}
	committed := make([]OutboxEntry, 0, len(deleted))
	for _, entry := range prepared {
		if _, ok := deletedSet[entry.Key]; !ok {
			if err := a.discardPreparedIntent(entry); err != nil {
				return nil, err
			}
			continue
		}
		committedEntry, err := a.commitPreparedIntent(entry, entry.PreviousVersion)
		if err != nil {
			return nil, err
		}
		committed = append(committed, committedEntry)
	}
	return committed, nil
}

func (a *Adapter) reconcilePreparedEntriesAfterError(ctx context.Context, entries []OutboxEntry, cause error) error {
	result := cause
	for _, entry := range entries {
		_, recoveryErr := a.reconcilePreparedEntryLocked(ctx, entry)
		result = errors.Join(result, recoveryErr)
	}
	return result
}

func (a *Adapter) propagateIntents(ctx context.Context, entries []OutboxEntry) error {
	for _, entry := range entries {
		if err := a.completeIntentLocked(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) (*storage.ObjectMeta, error) {
	return a.CopyObjectCond(ctx, storage.CopyRequest{
		SourceBucket: srcBucket,
		SourceKey:    srcKey,
		DestBucket:   dstBucket,
		DestKey:      dstKey,
	})
}

// CopyObjectCond is the conditional form, and it is here so a run-through server
// keeps the local store's copy semantics rather than quietly dropping to a
// head-then-copy: the conditions and the copied bytes would otherwise come from
// two different reads of the source.
//
// The outbox is written for the destination either way, so a refused copy
// propagates nothing — the local store's own precondition failure comes back
// before anything is prepared.
func (a *Adapter) CopyObjectCond(ctx context.Context, req storage.CopyRequest) (*storage.ObjectMeta, error) {
	srcBucket, srcKey := req.SourceBucket, req.SourceKey
	dstBucket, dstKey := req.DestBucket, req.DestKey
	action := a.decideUpstreamWrite(dstBucket)
	if action == writeError {
		return nil, ErrLiveWritesDisabled
	}
	if err := a.requireDurableOutbox(action); err != nil {
		return nil, err
	}
	var prepared OutboxEntry
	if action == writePropagate {
		unlock := a.outboxLocks.lock(outboxIdentity(dstBucket, dstKey))
		defer unlock()
		if err := a.rejectPreparedKey(dstBucket, dstKey); err != nil {
			return nil, err
		}
		previousVersion, err := a.localVersionStrict(ctx, dstBucket, dstKey)
		if err != nil {
			return nil, err
		}
		prepared, err = a.enqueuePreparedIntentLocked(OutboxCopy, dstBucket, dstKey, previousVersion, srcBucket, srcKey)
		if err != nil {
			return nil, err
		}
	}

	meta, err := a.localCopy(ctx, req)
	if err != nil {
		if prepared.ID != "" {
			return a.reconcilePreparedWrite(ctx, prepared, err)
		}
		return nil, err
	}
	a.invalidateCache(ctx, dstBucket, dstKey)
	if action == writePropagate {
		if _, err := a.commitPreparedIntent(prepared, objectVersion(meta)); err != nil {
			return meta, storage.CommittedError(err)
		}
		if err := a.completeIntentLocked(ctx, prepared); err != nil {
			return meta, storage.CommittedError(err)
		}
	}
	return meta, nil
}

// localCopy performs the copy against the local store, through whichever
// capability it offers, so the conditions are evaluated on the version the store
// copies rather than on one this layer read separately.
func (a *Adapter) localCopy(ctx context.Context, req storage.CopyRequest) (*storage.ObjectMeta, error) {
	if conditional, ok := a.local.(storage.ConditionalCopyStore); ok {
		return conditional.CopyObjectCond(ctx, req)
	}
	return a.local.CopyObject(ctx, req.SourceBucket, req.SourceKey, req.DestBucket, req.DestKey)
}

func (a *Adapter) UploadPart(ctx context.Context, uploadID string, partNumber int, body io.Reader) (*storage.PartInfo, error) {
	if a.localMultipart == nil {
		return nil, storage.ErrMultipartUnsupported
	}
	return a.localMultipart.UploadPart(ctx, uploadID, partNumber, body)
}

func (a *Adapter) CompleteMultipartUpload(ctx context.Context, uploadID string, parts []storage.PartInfo) (*storage.ObjectMeta, error) {
	if a.localMultipart == nil {
		return nil, storage.ErrMultipartUnsupported
	}
	upload, err := a.localMultipart.GetMultipartUpload(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	bucket, key := upload.Bucket, upload.Key
	action := a.decideUpstreamWrite(bucket)
	if action == writeError {
		return nil, ErrLiveWritesDisabled
	}
	if err := a.requireDurableOutbox(action); err != nil {
		return nil, err
	}
	var prepared OutboxEntry
	if action == writePropagate {
		unlock := a.outboxLocks.lock(outboxIdentity(bucket, key))
		defer unlock()
		if err := a.rejectPreparedKey(bucket, key); err != nil {
			return nil, err
		}
		previousVersion, err := a.localVersionStrict(ctx, bucket, key)
		if err != nil {
			return nil, err
		}
		prepared, err = a.enqueuePreparedIntentLocked(OutboxMultipart, bucket, key, previousVersion)
		if err != nil {
			return nil, err
		}
	}

	meta, err := a.localMultipart.CompleteMultipartUpload(ctx, uploadID, parts)
	if err != nil {
		if prepared.ID != "" {
			return a.reconcilePreparedWrite(ctx, prepared, err)
		}
		return nil, err
	}
	a.invalidateCache(ctx, bucket, key)
	if action == writePropagate {
		if _, err := a.commitPreparedIntent(prepared, objectVersion(meta)); err != nil {
			return meta, storage.CommittedError(err)
		}
		if err := a.completeIntentLocked(ctx, prepared); err != nil {
			return meta, storage.CommittedError(err)
		}
	}
	return meta, nil
}

func (a *Adapter) multipartTarget(ctx context.Context, uploadID string) (string, error) {
	upload, err := a.localMultipart.GetMultipartUpload(ctx, uploadID)
	if err != nil {
		return "", err
	}
	return upload.Bucket, nil
}
