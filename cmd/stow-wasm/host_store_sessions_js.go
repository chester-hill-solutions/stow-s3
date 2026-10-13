package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type runtimeReadSession struct {
	mu     sync.Mutex
	handle int
	reader io.ReadCloser
	cancel context.CancelFunc
}

type runtimeWriteSession struct {
	mu       sync.Mutex
	handle   int
	writer   *io.PipeWriter
	cancel   context.CancelFunc
	size     int64
	accepted int64
	done     chan struct{}
	object   stow.Object
	err      error
}

var runtimeStreams = struct {
	sync.Mutex
	reads   map[string]*runtimeReadSession
	writes  map[string]*runtimeWriteSession
	pending map[string]pendingStream
	closing map[int]bool
}{reads: make(map[string]*runtimeReadSession), writes: make(map[string]*runtimeWriteSession), pending: make(map[string]pendingStream), closing: make(map[int]bool)}

type pendingStream struct {
	handle int
	cancel context.CancelFunc
}

func streamToken(ctx context.Context, handle int) (string, context.Context, context.CancelFunc, error) {
	runtimeStreams.Lock()
	defer runtimeStreams.Unlock()
	if runtimeStreams.closing[handle] {
		return "", nil, nil, stow.ErrClosed
	}
	count := 0
	for _, session := range runtimeStreams.reads {
		if session.handle == handle {
			count++
		}
	}
	for _, session := range runtimeStreams.writes {
		if session.handle == handle {
			count++
		}
	}
	for _, session := range runtimeStreams.pending {
		if session.handle == handle {
			count++
		}
	}
	if count >= 16 {
		return "", nil, nil, stow.ErrQuotaExceeded
	}
	token, err := storage.NewRecordVersion()
	if err != nil {
		return "", nil, nil, err
	}
	streamContext, cancel := context.WithCancel(ctx)
	runtimeStreams.pending[token] = pendingStream{handle: handle, cancel: cancel}
	return token, streamContext, cancel, nil
}

func releasePendingStream(token string) {
	runtimeStreams.Lock()
	pending, found := runtimeStreams.pending[token]
	delete(runtimeStreams.pending, token)
	runtimeStreams.Unlock()
	if found {
		pending.cancel()
	}
}

func handleOpenObjectRead(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	if !req.async {
		return nil, fmt.Errorf("object streams require asynchronous dispatch")
	}
	token, streamContext, cancel, err := streamToken(ctx, req.Handle)
	if err != nil {
		return nil, err
	}
	defer releasePendingStream(token)
	reader, object, err := instance.OpenObject(streamContext, req.Bucket, req.Key)
	if err != nil {
		cancel()
		return nil, err
	}
	runtimeStreams.Lock()
	if err := streamContext.Err(); err != nil {
		runtimeStreams.Unlock()
		_ = reader.Close()
		return nil, err
	}
	runtimeStreams.reads[token] = &runtimeReadSession{handle: req.Handle, reader: reader, cancel: cancel}
	delete(runtimeStreams.pending, token)
	runtimeStreams.Unlock()
	return marshalResult(struct {
		Token  string       `json:"token"`
		Object objectResult `json:"object"`
	}{token, objectResultOf(object)})
}

func handleReadObjectChunk(_ context.Context, _ *stow.Runtime, req request) (json.RawMessage, error) {
	if req.Length < 1 || req.Length > hostChunkLimit || req.binaryOutput == nil {
		return nil, fmt.Errorf("invalid stream chunk length")
	}
	runtimeStreams.Lock()
	session := runtimeStreams.reads[req.Token]
	runtimeStreams.Unlock()
	if session == nil || session.handle != req.Handle {
		return nil, fmt.Errorf("object stream is not open")
	}
	session.mu.Lock()
	buffer := make([]byte, req.Length)
	n, err := session.reader.Read(buffer)
	session.mu.Unlock()
	if err != nil && err != io.EOF {
		closeReadSession(req.Token, req.Handle)
		return nil, err
	}
	*req.binaryOutput = buffer[:n]
	if err == io.EOF {
		closeReadSession(req.Token, req.Handle)
	}
	return marshalResult(struct {
		EOF bool `json:"eof"`
	}{err == io.EOF})
}

func closeReadSession(token string, handle int) {
	runtimeStreams.Lock()
	session := runtimeStreams.reads[token]
	if session != nil && session.handle == handle {
		delete(runtimeStreams.reads, token)
	} else {
		session = nil
	}
	runtimeStreams.Unlock()
	if session != nil {
		session.cancel()
		_ = session.reader.Close()
	}
}

func handleCloseObjectRead(_ context.Context, _ *stow.Runtime, req request) (json.RawMessage, error) {
	closeReadSession(req.Token, req.Handle)
	return nil, nil
}

func handleOpenObjectWrite(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	if !req.async || req.Size == nil || *req.Size < 0 {
		return nil, fmt.Errorf("stream writes require asynchronous dispatch and a non-negative declared size")
	}
	token, streamContext, cancel, err := streamToken(ctx, req.Handle)
	if err != nil {
		return nil, err
	}
	defer releasePendingStream(token)
	reader, writer := io.Pipe()
	session := &runtimeWriteSession{handle: req.Handle, writer: writer, cancel: cancel, size: *req.Size, done: make(chan struct{})}
	runtimeStreams.Lock()
	if err := streamContext.Err(); err != nil {
		runtimeStreams.Unlock()
		_ = reader.Close()
		_ = writer.Close()
		return nil, err
	}
	runtimeStreams.writes[token] = session
	delete(runtimeStreams.pending, token)
	runtimeStreams.Unlock()
	go func() {
		session.object, session.err = instance.PutObjectStream(streamContext, req.Bucket, req.Key, reader, stow.StreamPutOptions{Size: *req.Size, PutOptions: stow.PutOptions{ContentType: req.ContentType, Metadata: req.Metadata, IfMatch: req.IfMatch, IfNoneMatch: req.IfNoneMatch}})
		close(session.done)
		_ = reader.CloseWithError(session.err)
	}()
	return marshalResult(hostToken{Token: token})
}

func findWriteSession(req request) (*runtimeWriteSession, error) {
	runtimeStreams.Lock()
	session := runtimeStreams.writes[req.Token]
	runtimeStreams.Unlock()
	if session == nil || session.handle != req.Handle {
		return nil, fmt.Errorf("object write is not open")
	}
	return session, nil
}

func handleWriteObjectChunk(_ context.Context, _ *stow.Runtime, req request) (json.RawMessage, error) {
	session, err := findWriteSession(req)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if len(req.binaryData) > hostChunkLimit || int64(len(req.binaryData)) > session.size-session.accepted {
		return nil, fmt.Errorf("stream chunk exceeds bound or declared size")
	}
	n, err := session.writer.Write(req.binaryData)
	session.accepted += int64(n)
	if err != nil {
		select {
		case <-session.done:
			if session.err != nil {
				return nil, session.err
			}
		default:
		}
		return nil, err
	}
	return nil, nil
}

func removeWriteSession(token string, session *runtimeWriteSession) {
	runtimeStreams.Lock()
	delete(runtimeStreams.writes, token)
	runtimeStreams.Unlock()
	session.cancel()
}

func handleCommitObjectWrite(_ context.Context, _ *stow.Runtime, req request) (json.RawMessage, error) {
	session, err := findWriteSession(req)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	_ = session.writer.Close()
	<-session.done
	removeWriteSession(req.Token, session)
	if session.err != nil {
		return nil, session.err
	}
	return marshalObjectResult(session.object, req)
}

func handleAbortObjectWrite(_ context.Context, _ *stow.Runtime, req request) (json.RawMessage, error) {
	session, err := findWriteSession(req)
	if err != nil {
		return nil, nil
	}
	session.cancel()
	_ = session.writer.CloseWithError(context.Canceled)
	<-session.done
	removeWriteSession(req.Token, session)
	return nil, nil
}

func closeRuntimeStreams(handle int) {
	runtimeStreams.Lock()
	runtimeStreams.closing[handle] = true
	pending := make([]context.CancelFunc, 0)
	for _, session := range runtimeStreams.pending {
		if session.handle == handle {
			pending = append(pending, session.cancel)
		}
	}
	reads := make([]string, 0)
	writes := make([]string, 0)
	for token, session := range runtimeStreams.reads {
		if session.handle == handle {
			reads = append(reads, token)
		}
	}
	for token, session := range runtimeStreams.writes {
		if session.handle == handle {
			writes = append(writes, token)
		}
	}
	runtimeStreams.Unlock()
	for _, cancel := range pending {
		cancel()
	}
	for _, token := range reads {
		closeReadSession(token, handle)
	}
	for _, token := range writes {
		_, _ = handleAbortObjectWrite(context.Background(), nil, request{Handle: handle, Token: token})
	}
}

func releaseRuntimeStreamHandle(handle int) {
	runtimeStreams.Lock()
	delete(runtimeStreams.closing, handle)
	runtimeStreams.Unlock()
}
