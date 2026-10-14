package storage

import "io"

// ByteReader exposes borrowed, read-only bytes. It does not transfer ownership.
// PreparePut copies ordinary readers; only an explicit OwnedBody can be adopted.
type ByteReader interface {
	io.Reader
	// Bytes returns the reader's content. The returned slice is owned by the
	// receiver and must not be modified by the caller.
	Bytes() []byte
}

// BytesOf returns a reader's content without copying it when the reader already
// holds it in memory, and by reading it once when it does not. The returned
// bytes may be borrowed from the reader and must be treated as read-only.
func BytesOf(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	if holder, ok := r.(ByteReader); ok {
		return holder.Bytes(), nil
	}
	return readOwnedBytes(r)
}
