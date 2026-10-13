package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type hostToken struct {
	Token string `json:"token"`
}

func (s *hostStore) PutObject(ctx context.Context, bucket, key string, data []byte, options stow.PutOptions) (stow.Object, error) {
	return s.PutObjectStream(ctx, bucket, key, bytes.NewReader(data), options)
}

func (s *hostStore) PutObjectStream(ctx context.Context, bucket, key string, body io.Reader, options stow.PutOptions) (stow.Object, error) {
	pending, err := s.StageObject(ctx, bucket, key, body, options)
	if err != nil {
		return stow.Object{}, err
	}
	defer pending.Abort(context.Background())
	return pending.Commit(ctx)
}

type hostPendingObject struct {
	store     *hostStore
	token     string
	size      int64
	etag      string
	committed bool
}

func (p *hostPendingObject) Commit(ctx context.Context) (stow.Object, error) {
	result, _, err := p.store.invoke(ctx, hostRequest{Op: "commitPut", Token: p.token, Size: p.size, ETag: p.etag, LastModified: time.Now().UTC().Format(time.RFC3339Nano)}, nil)
	if err != nil {
		return stow.Object{}, err
	}
	p.committed = true
	return decodeHostObject(result)
}
func (p *hostPendingObject) Abort(ctx context.Context) error {
	if p.committed {
		return nil
	}
	_, _, err := p.store.invoke(ctx, hostRequest{Op: "abortPut", Token: p.token}, nil)
	return err
}

func (s *hostStore) StageObject(ctx context.Context, bucket, key string, body io.Reader, options stow.PutOptions) (stow.StagedObject, error) {
	if err := validateHostObject(bucket, key); err != nil {
		return nil, err
	}
	result, _, err := s.invoke(ctx, hostRequest{Op: "startPut", Bucket: bucket, Key: key, ContentType: options.ContentType, Metadata: options.Metadata, IfMatch: options.IfMatch, IfNoneMatch: options.IfNoneMatch}, nil)
	if err != nil {
		return nil, err
	}
	var upload hostToken
	if err := json.Unmarshal(result, &upload); err != nil {
		return nil, err
	}
	if upload.Token == "" {
		return nil, fmt.Errorf("host upload has no token")
	}
	committed := false
	defer func() {
		if !committed {
			_, _, _ = s.invoke(context.Background(), hostRequest{Op: "abortPut", Token: upload.Token}, nil)
		}
	}()
	digest := md5.New()
	buffer := make([]byte, hostChunkLimit)
	var size int64
	for {
		n, readErr := io.ReadFull(body, buffer)
		if n > 0 {
			if _, _, err := s.invoke(ctx, hostRequest{Op: "writePut", Token: upload.Token, Offset: size}, buffer[:n]); err != nil {
				return nil, err
			}
			_, _ = digest.Write(buffer[:n])
			size += int64(n)
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	etag := "\"" + hex.EncodeToString(digest.Sum(nil)) + "\""
	committed = true
	return &hostPendingObject{store: s, token: upload.Token, size: size, etag: etag}, nil
}

func (s *hostStore) GetObject(ctx context.Context, bucket, key string) (stow.Object, error) {
	reader, object, err := s.OpenObject(ctx, bucket, key)
	if err != nil {
		return stow.Object{}, err
	}
	defer reader.Close()
	object.Data, err = io.ReadAll(reader)
	return object, err
}

func (s *hostStore) OpenObject(ctx context.Context, bucket, key string) (io.ReadCloser, stow.Object, error) {
	if err := validateHostObject(bucket, key); err != nil {
		return nil, stow.Object{}, err
	}
	result, _, err := s.invoke(ctx, hostRequest{Op: "openObject", Bucket: bucket, Key: key}, nil)
	if err != nil {
		return nil, stow.Object{}, err
	}
	var opened struct {
		Token  string          `json:"token"`
		Object json.RawMessage `json:"object"`
	}
	if err := json.Unmarshal(result, &opened); err != nil {
		return nil, stow.Object{}, err
	}
	if opened.Token == "" {
		return nil, stow.Object{}, fmt.Errorf("host reader has no token")
	}
	object, err := decodeHostObject(opened.Object)
	if err != nil {
		_, _, _ = s.invoke(context.Background(), hostRequest{Op: "closeObject", Token: opened.Token}, nil)
		return nil, stow.Object{}, err
	}
	reader := &hostReader{store: s, context: ctx, token: opened.Token, end: object.Size}
	s.mu.Lock()
	s.readers[reader.token] = reader
	s.mu.Unlock()
	return reader, object, nil
}

func (s *hostStore) OpenObjectRange(ctx context.Context, bucket, key string, span stow.ObjectRange) (io.ReadCloser, stow.Object, error) {
	reader, object, err := s.OpenObject(ctx, bucket, key)
	if err != nil {
		return nil, stow.Object{}, err
	}
	if span.Offset < 0 || span.Length < 0 || span.Offset > object.Size || span.Length > object.Size-span.Offset {
		_ = reader.Close()
		return nil, stow.Object{}, fmt.Errorf("object range exceeds object size")
	}
	ranged := reader.(*hostReader)
	ranged.offset = span.Offset
	ranged.end = span.Offset + span.Length
	return ranged, object, nil
}

type hostReader struct {
	mu      sync.Mutex
	store   *hostStore
	context context.Context
	token   string
	offset  int64
	end     int64
	closed  bool
}

func (r *hostReader) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if r.offset == r.end {
		return 0, io.EOF
	}
	length := int64(len(buffer))
	if length > hostChunkLimit {
		length = hostChunkLimit
	}
	if length > r.end-r.offset {
		length = r.end - r.offset
	}
	_, data, err := r.store.invoke(r.context, hostRequest{Op: "readObject", Token: r.token, Offset: r.offset, Length: length}, nil)
	if err != nil {
		return 0, err
	}
	if int64(len(data)) != length {
		return 0, io.ErrUnexpectedEOF
	}
	copy(buffer, data)
	r.offset += length
	return len(data), nil
}

func (r *hostReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.store.mu.Lock()
	delete(r.store.readers, r.token)
	r.store.mu.Unlock()
	_, _, err := r.store.invoke(context.Background(), hostRequest{Op: "closeObject", Token: r.token}, nil)
	return err
}
