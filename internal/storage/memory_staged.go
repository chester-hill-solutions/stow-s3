package storage

import (
	"context"
	"errors"
	"sync"
)

var _ StagedWriteStore = (*MemoryStore)(nil)

type memoryStagedObject struct {
	mu          sync.Mutex
	store       *MemoryStore
	bucket, key string
	prepared    *PreparedPut
}

// Commit transfers the prepared payload once, checking conditions at publication.
func (p *memoryStagedObject) Commit(ctx context.Context) (*ObjectMeta, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prepared == nil {
		return nil, errors.New("memory object is no longer staged")
	}
	defer func() { p.prepared = nil }()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return p.store.commitMemoryPut(p)
}

func (p *memoryStagedObject) Abort(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prepared = nil
	return nil
}
