package storage

import (
	"bytes"
	"io"
)

// PreparedPut owns the bytes and normalized properties of a buffered object write.
type PreparedPut struct {
	Data    []byte
	ETag    string
	Options PutOptions
}

// PreparePut consumes the reader, verifies integrity and copies retained metadata.
// Reader size hints affect allocation only; protocol length checks stay with callers.
func PreparePut(body io.Reader, opts PutOptions) (PreparedPut, error) {
	if body == nil {
		body = bytes.NewReader(nil)
	}
	var data []byte
	var err error
	if owned, ok := body.(*OwnedBody); ok {
		data, err = owned.take()
	} else {
		data, err = readOwnedBytes(body)
	}
	if err != nil {
		return PreparedPut{}, err
	}
	opts, err = ResolvePutChecksum(opts, data)
	if err != nil {
		return PreparedPut{}, err
	}
	opts.Metadata = CloneMetadata(opts.Metadata)
	return PreparedPut{Data: data, ETag: ETagForBytes(data), Options: opts}, nil
}
