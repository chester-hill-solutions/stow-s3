package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"syscall/js"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

const hostChunkLimit = 256 << 10

type hostStore struct {
	host       js.Value
	handle     int
	mu         sync.Mutex
	closed     bool
	readers    map[string]*hostReader
	conditions js.Func
}

var hostHandles = struct {
	sync.Mutex
	values map[int]bool
}{values: make(map[int]bool)}

func isHostHandle(handle int) bool {
	hostHandles.Lock()
	defer hostHandles.Unlock()
	return hostHandles.values[handle]
}

func openHostRuntime(req request, host js.Value) (json.RawMessage, error) {
	store := &hostStore{host: host, readers: make(map[string]*hostReader)}
	store.conditions = js.FuncOf(hostCheckConditions)
	host.Set("checkConditions", store.conditions)
	options := req.Options
	options.Store = store
	options.Backend = stow.BackendHost
	instance, err := stow.Open(options)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	state.Lock()
	handle := state.next
	state.next++
	state.instances[handle] = instance
	store.handle = handle
	state.Unlock()
	hostHandles.Lock()
	hostHandles.values[handle] = true
	hostHandles.Unlock()
	return marshalResult(openResult{Handle: handle, Capabilities: capabilities(instance.Capabilities())})
}

type hostRequest struct {
	Version           int               `json:"version"`
	Op                string            `json:"op"`
	Bucket            string            `json:"bucket,omitempty"`
	Key               string            `json:"key,omitempty"`
	Token             string            `json:"token,omitempty"`
	Offset            int64             `json:"offset,omitempty"`
	Length            int64             `json:"length,omitempty"`
	Size              int64             `json:"size"`
	ETag              string            `json:"etag,omitempty"`
	LastModified      string            `json:"lastModified,omitempty"`
	ContentType       string            `json:"contentType,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	IfMatch           string            `json:"ifMatch,omitempty"`
	IfNoneMatch       string            `json:"ifNoneMatch,omitempty"`
	List              *hostListOptions  `json:"list,omitempty"`
	SourceBucket      string            `json:"sourceBucket,omitempty"`
	SourceKey         string            `json:"sourceKey,omitempty"`
	DestinationBucket string            `json:"destinationBucket,omitempty"`
	DestinationKey    string            `json:"destinationKey,omitempty"`
}

type hostListOptions struct {
	Prefix string `json:"prefix"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

func (s *hostStore) invoke(ctx context.Context, req hostRequest, data []byte) (json.RawMessage, []byte, error) {
	req.Version = binaryProtocolVersion
	metadata, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}
	input := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(input, data)
	value, err := awaitHostPromise(ctx, s.host.Call("call", string(metadata), input))
	if err != nil {
		return nil, nil, err
	}
	if value.Type() != js.TypeObject || value.Get("metadata").Type() != js.TypeString {
		return nil, nil, fmt.Errorf("host store returned an invalid response")
	}
	var reply response
	if err := json.Unmarshal([]byte(value.Get("metadata").String()), &reply); err != nil {
		return nil, nil, err
	}
	if reply.Version != binaryProtocolVersion {
		return nil, nil, fmt.Errorf("host store returned an unsupported response version")
	}
	if !reply.OK {
		return nil, nil, hostStoreError(reply.Error)
	}
	var output []byte
	payload := value.Get("data")
	if payload.Type() != js.TypeUndefined && payload.Type() != js.TypeNull {
		if !payload.InstanceOf(js.Global().Get("Uint8Array")) || payload.Length() > hostChunkLimit {
			return nil, nil, fmt.Errorf("host store returned an invalid or oversized payload")
		}
		output = make([]byte, payload.Length())
		js.CopyBytesToGo(output, payload)
	}
	return reply.Result, output, nil
}

func hostStoreError(value *errorPayload) error {
	if value == nil {
		return fmt.Errorf("host store failed without an error")
	}
	var cause error
	switch value.Code {
	case "bucket_not_found":
		cause = stow.ErrBucketNotFound
	case "bucket_exists":
		cause = stow.ErrBucketExists
	case "bucket_not_empty":
		cause = stow.ErrBucketNotEmpty
	case "object_not_found":
		cause = stow.ErrObjectNotFound
	case "precondition_failed":
		cause = stow.ErrPreconditionFailed
	case "quota_exceeded":
		cause = stow.ErrQuotaExceeded
	case "closed":
		cause = stow.ErrClosed
	default:
		return fmt.Errorf("host store %s: %s", value.Code, value.Message)
	}
	return fmt.Errorf("%w: %s", cause, value.Message)
}

func validateHostObject(bucket, key string) error {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return stow.ErrInvalidBucket
	}
	if err := storage.ValidateKey(key); err != nil {
		return stow.ErrInvalidKey
	}
	return nil
}

func (s *hostStore) CreateBucket(ctx context.Context, name string) error {
	if err := storage.ValidateBucketName(name); err != nil {
		return stow.ErrInvalidBucket
	}
	_, _, err := s.invoke(ctx, hostRequest{Op: "createBucket", Bucket: name}, nil)
	return err
}

func (s *hostStore) DeleteBucket(ctx context.Context, name string) error {
	if err := storage.ValidateBucketName(name); err != nil {
		return stow.ErrInvalidBucket
	}
	_, _, err := s.invoke(ctx, hostRequest{Op: "deleteBucket", Bucket: name}, nil)
	return err
}

func (s *hostStore) ListBuckets(ctx context.Context) ([]stow.Bucket, error) {
	result, _, err := s.invoke(ctx, hostRequest{Op: "listBuckets"}, nil)
	if err != nil {
		return nil, err
	}
	var list bucketListResult
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, err
	}
	buckets := make([]stow.Bucket, 0, len(list.Buckets))
	for _, item := range list.Buckets {
		created, err := parseHostTime(item.CreationDate)
		if err != nil {
			return nil, err
		}
		buckets = append(buckets, stow.Bucket{Name: item.Name, CreationDate: created})
	}
	return buckets, nil
}

func decodeHostObject(data json.RawMessage) (stow.Object, error) {
	var result objectResult
	if err := json.Unmarshal(data, &result); err != nil {
		return stow.Object{}, err
	}
	if result.Size < 0 || result.Data != "" {
		return stow.Object{}, fmt.Errorf("host object metadata is invalid")
	}
	modified, err := parseHostTime(result.LastModified)
	if err != nil {
		return stow.Object{}, err
	}
	return stow.Object{Bucket: result.Bucket, Key: result.Key, Size: result.Size, ETag: result.ETag, ContentType: result.ContentType, Metadata: result.Metadata, LastModified: modified}, nil
}

func parseHostTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}

func (s *hostStore) HeadObject(ctx context.Context, bucket, key string) (stow.Object, error) {
	if err := validateHostObject(bucket, key); err != nil {
		return stow.Object{}, err
	}
	result, _, err := s.invoke(ctx, hostRequest{Op: "headObject", Bucket: bucket, Key: key}, nil)
	if err != nil {
		return stow.Object{}, err
	}
	return decodeHostObject(result)
}

func (s *hostStore) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := validateHostObject(bucket, key); err != nil {
		return err
	}
	_, _, err := s.invoke(ctx, hostRequest{Op: "deleteObject", Bucket: bucket, Key: key}, nil)
	return err
}

func (s *hostStore) ListObjects(ctx context.Context, bucket string, options stow.ListOptions) (stow.ObjectPage, error) {
	if err := storage.ValidateBucketName(bucket); err != nil {
		return stow.ObjectPage{}, stow.ErrInvalidBucket
	}
	listOptions := hostListOptions{Prefix: options.Prefix, Cursor: options.Cursor, Limit: options.Limit}
	result, _, err := s.invoke(ctx, hostRequest{Op: "listObjects", Bucket: bucket, List: &listOptions}, nil)
	if err != nil {
		return stow.ObjectPage{}, err
	}
	var list listResult
	if err := json.Unmarshal(result, &list); err != nil {
		return stow.ObjectPage{}, err
	}
	page := stow.ObjectPage{Truncated: list.Truncated, NextCursor: list.NextCursor}
	for _, item := range list.Objects {
		encoded, err := json.Marshal(item)
		if err != nil {
			return stow.ObjectPage{}, err
		}
		object, err := decodeHostObject(encoded)
		if err != nil {
			return stow.ObjectPage{}, err
		}
		page.Objects = append(page.Objects, object)
	}
	return page, nil
}

func (s *hostStore) CopyObject(ctx context.Context, sourceBucket, sourceKey, destinationBucket, destinationKey string) (stow.Object, error) {
	if err := validateHostObject(sourceBucket, sourceKey); err != nil {
		return stow.Object{}, err
	}
	if err := validateHostObject(destinationBucket, destinationKey); err != nil {
		return stow.Object{}, err
	}
	result, _, err := s.invoke(ctx, hostRequest{Op: "copyObject", SourceBucket: sourceBucket, SourceKey: sourceKey, DestinationBucket: destinationBucket, DestinationKey: destinationKey}, nil)
	if err != nil {
		return stow.Object{}, err
	}
	return decodeHostObject(result)
}

func (s *hostStore) SupportsConditionalWrites() bool { return true }

func (s *hostStore) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	readers := make([]*hostReader, 0, len(s.readers))
	for _, reader := range s.readers {
		readers = append(readers, reader)
	}
	s.mu.Unlock()
	for _, reader := range readers {
		_ = reader.Close()
	}
	hostHandles.Lock()
	delete(hostHandles.values, s.handle)
	hostHandles.Unlock()
	_, _, err := s.invoke(context.Background(), hostRequest{Op: "close"}, nil)
	s.host.Set("checkConditions", js.Undefined())
	s.conditions.Release()
	return err
}
