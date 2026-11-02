package runthrough

import (
	"context"
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/atomicfile"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// UpstreamState survives cache eviction, successful propagation, and restart.
// An empty ETag without Absent is unknown and cannot authorize propagation.
type UpstreamState struct {
	ETag    string `json:"etag,omitempty"`
	Absent  bool   `json:"absent,omitempty"`
	Unknown bool   `json:"unknown,omitempty"`
}

type provenanceOutbox interface {
	UpstreamState(bucket, key string) (UpstreamState, bool, error)
	SaveUpstreamState(bucket, key string, state UpstreamState) error
}

func (o *FileOutbox) UpstreamState(bucket, key string) (UpstreamState, bool, error) {
	var value UpstreamState
	var known bool
	err := o.withLockedState(func(s outboxState) error { value, known = s.provenance[outboxIdentity(bucket, key)]; return nil })
	return value, known, err
}

func (o *FileOutbox) SaveUpstreamState(bucket, key string, value UpstreamState) error {
	return o.withLockedState(func(state outboxState) error {
		id := outboxIdentity(bucket, key)
		if previous, known := state.provenance[id]; known && previous == value && o.nativeJournal {
			if err := atomicfile.SyncExisting(o.path, 0o600); err == nil {
				return nil
			}
		}
		// Full replacement also repairs incompatible files or a failed existing-file fence.
		next := state.clone()
		next.provenance[id] = value
		return o.commitState(next)
	})
}

func (o *MemoryOutbox) UpstreamState(bucket, key string) (UpstreamState, bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	value, ok := o.state.provenance[outboxIdentity(bucket, key)]
	return value, ok, nil
}

func (o *MemoryOutbox) SaveUpstreamState(bucket, key string, value UpstreamState) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.state.provenance[outboxIdentity(bucket, key)] = value
	return nil
}

func (a *Adapter) saveUpstreamState(bucket, key string, value UpstreamState) error {
	if value.ETag == "" && !value.Absent && !value.Unknown {
		return errors.New("upstream returned no ETag")
	}
	provider, ok := a.outbox.(provenanceOutbox)
	if !ok {
		return errors.New("outbox cannot retain upstream provenance")
	}
	return provider.SaveUpstreamState(bucket, key, value)
}

func (a *Adapter) recordUpstreamState(ctx context.Context, entry *OutboxEntry) error {
	if !a.upstreamReachable(entry.Bucket) {
		return nil
	}
	provider, ok := a.outbox.(provenanceOutbox)
	if !ok {
		return errors.New("outbox cannot retain upstream provenance")
	}
	state, known, err := provider.UpstreamState(entry.Bucket, entry.Key)
	if err != nil {
		return err
	}
	if !known {
		meta, err := a.upstream.HeadObject(ctx, entry.Bucket, entry.Key)
		switch {
		case errors.Is(err, storage.ErrObjectNotFound):
			state.Absent = true
		case err != nil:
			// The local store remains authoritative when upstream is unavailable.
			// Persist unknown explicitly so a later retry cannot turn the outage
			// into an unguarded last-writer-wins operation.
			state.Unknown = true
		case meta == nil || meta.ETag == "":
			return errors.New("upstream returned no ETag")
		default:
			state.ETag = meta.ETag
		}
		if err := provider.SaveUpstreamState(entry.Bucket, entry.Key, state); err != nil {
			return err
		}
	}
	entry.UpstreamVersion, entry.UpstreamAbsent = state.ETag, state.Absent
	if state.Unknown {
		entry.UpstreamVersion, entry.UpstreamAbsent = "", false
	}
	return nil
}

func (a *Adapter) refreshUpstreamState(ctx context.Context, bucket, key string) {
	if !a.upstreamReachable(bucket) {
		return
	}
	provider, ok := a.outbox.(provenanceOutbox)
	if !ok {
		return
	}
	state := UpstreamState{}
	meta, err := a.upstream.HeadObject(ctx, bucket, key)
	switch {
	case errors.Is(err, storage.ErrObjectNotFound):
		state.Absent = true
	case err != nil:
		state.Unknown = true
	case meta == nil || meta.ETag == "":
		state.Unknown = true
	default:
		state.ETag = meta.ETag
	}
	_ = provider.SaveUpstreamState(bucket, key, state)
}
