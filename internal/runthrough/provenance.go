package runthrough

import (
	"context"
	"errors"

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
	s, err := o.readState()
	value, ok := s.provenance[outboxIdentity(bucket, key)]
	return value, ok, err
}

func (o *FileOutbox) SaveUpstreamState(bucket, key string, value UpstreamState) error {
	return o.withState(func(s *outboxState) error { s.provenance[outboxIdentity(bucket, key)] = value; return nil })
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
	if !a.upstreamEnabled(entry.Bucket) {
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
