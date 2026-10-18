package storage

import (
	"errors"
	"io"
	"sync"
)

var errOwnedBodyTaken = errors.New("owned request body was already transferred")

// OwnedBody transfers an internally owned buffer once; ordinary readers stay borrowed.
type OwnedBody struct {
	mu     sync.Mutex
	data   []byte
	offset int
	taken  bool
}

// NewOwnedBody relinquishes the caller's buffer. Its aliases must never be mutated again.
func NewOwnedBody(data []byte) *OwnedBody { return &OwnedBody{data: data} }

func (b *OwnedBody) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.data) - b.offset
}

func (b *OwnedBody) Read(buffer []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.taken {
		return 0, errOwnedBodyTaken
	}
	if b.offset == len(b.data) {
		return 0, io.EOF
	}
	n := copy(buffer, b.data[b.offset:])
	b.offset += n
	return n, nil
}

func (b *OwnedBody) take() ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.taken {
		return nil, errOwnedBodyTaken
	}
	data := b.data[b.offset:]
	b.data, b.offset, b.taken = nil, 0, true
	return data, nil
}
