//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"syscall/js"

	stow "github.com/chester-hill-solutions/stow-s3/internal/embedded"
)

const protocolVersion = 1

type request struct {
	Version           int          `json:"version"`
	Op                string       `json:"op"`
	Handle            int          `json:"handle"`
	Options           stow.Options `json:"options"`
	Bucket            string       `json:"bucket"`
	Key               string       `json:"key"`
	Data              string       `json:"data"`
	binaryData        []byte
	binaryOutput      *[]byte
	binaryBuffer      **[]byte
	async             bool
	ContentType       string            `json:"contentType"`
	Metadata          map[string]string `json:"metadata"`
	IfMatch           string            `json:"ifMatch"`
	IfNoneMatch       string            `json:"ifNoneMatch"`
	List              stow.ListOptions  `json:"list"`
	Range             stow.ObjectRange  `json:"range"`
	Token             string            `json:"token"`
	ObservationToken  string            `json:"observationToken"`
	RequestKey        string            `json:"requestKey"`
	Replace           bool              `json:"replace"`
	Length            int               `json:"length"`
	Size              *int64            `json:"size"`
	SourceBucket      string            `json:"sourceBucket"`
	SourceKey         string            `json:"sourceKey"`
	DestinationBucket string            `json:"destinationBucket"`
	DestinationKey    string            `json:"destinationKey"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type response struct {
	Version int             `json:"version"`
	OK      bool            `json:"ok"`
	Error   *errorPayload   `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

type openResult struct {
	Handle       int                `json:"handle"`
	Capabilities capabilitiesResult `json:"capabilities"`
}

type capabilitiesResult struct {
	Backend             stow.Backend `json:"backend"`
	MaxBytes            int64        `json:"maxBytes"`
	MaxObjects          int64        `json:"maxObjects"`
	Persistent          bool         `json:"persistent"`
	Multipart           bool         `json:"multipart"`
	Upstream            bool         `json:"upstream"`
	ConditionalWrites   bool         `json:"conditionalWrites"`
	GuardedSaves        bool         `json:"guardedSaves"`
	DurableSaveRequests bool         `json:"durableSaveRequests"`
	RecoveryHolds       bool         `json:"recoveryHolds"`
}

type objectResult struct {
	Bucket       string            `json:"bucket"`
	Key          string            `json:"key"`
	Data         string            `json:"data,omitempty"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"contentType,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	LastModified string            `json:"lastModified,omitempty"`
}

type listResult struct {
	Objects    []objectResult `json:"objects"`
	Truncated  bool           `json:"truncated"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type bucketResult struct {
	Name         string `json:"name"`
	CreationDate string `json:"creationDate,omitempty"`
}

type bucketListResult struct {
	Buckets []bucketResult `json:"buckets"`
}

type usageResult struct {
	Bytes   int64 `json:"bytes"`
	Objects int64 `json:"objects"`
}

type readForSaveResult struct {
	Object           objectResult `json:"object"`
	Absent           bool         `json:"absent"`
	ObservationToken string       `json:"observationToken"`
}

type saveResult struct {
	Object   objectResult `json:"object"`
	Outcome  string       `json:"outcome"`
	Replayed bool         `json:"replayed"`
}

type handler func(context.Context, *stow.Runtime, request) (json.RawMessage, error)

var state = struct {
	sync.Mutex
	next         int
	instances    map[int]*stow.Runtime
	observations map[int]*observationTable
}{instances: make(map[int]*stow.Runtime), observations: make(map[int]*observationTable)}

// observationsFor is the retained conditions of one runtime. A condition is bound
// to the instance that observed it, so the table is per handle.
func observationsFor(handle int) *observationTable {
	state.Lock()
	defer state.Unlock()
	table, ok := state.observations[handle]
	if !ok {
		table = newObservationTable()
		state.observations[handle] = table
	}
	return table
}

func dropObservations(handle int) {
	state.Lock()
	defer state.Unlock()
	if table, ok := state.observations[handle]; ok {
		table.clear()
		delete(state.observations, handle)
	}
}

func main() {
	state.Lock()
	state.next = 1
	state.Unlock()
	exit := make(chan struct{})
	var exitOnce sync.Once
	exitFunc := js.FuncOf(func(js.Value, []js.Value) interface{} {
		exitOnce.Do(func() { close(exit) })
		return nil
	})
	callFunc := js.FuncOf(call)
	binaryFunc := js.FuncOf(callBinary)
	asyncFunc := js.FuncOf(callBinaryAsync)
	openHostFunc := js.FuncOf(openHost)
	js.Global().Set("stow", js.ValueOf(map[string]interface{}{"call": callFunc, "callBinary": binaryFunc, "callBinaryAsync": asyncFunc, "openHost": openHostFunc, "exit": exitFunc}))
	<-exit
}

func call(_ js.Value, args []js.Value) interface{} {
	if len(args) != 1 {
		return encodeResponse(failure("invalid_request", "expected one JSON request"))
	}
	var req request
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		return encodeResponse(failure("invalid_request", "invalid JSON request: "+err.Error()))
	}
	if req.Version != protocolVersion {
		return encodeResponse(failure("unsupported_protocol", fmt.Sprintf("unsupported protocol version %d", req.Version)))
	}
	result, err := dispatch(req)
	if err != nil {
		return encodeResponse(errorResponse(err))
	}
	return encodeResponse(response{Version: protocolVersion, OK: true, Result: result})
}

func dispatch(req request) (json.RawMessage, error) {
	if req.Op == "open" {
		return openRuntime(req)
	}
	if !req.async && isHostHandle(req.Handle) {
		return nil, fmt.Errorf("host-backed runtime requires asynchronous calls")
	}
	state.Lock()
	instance, ok := state.instances[req.Handle]
	state.Unlock()
	if !ok {
		return nil, fmt.Errorf("runtime handle %d is not open", req.Handle)
	}
	operation, ok := operationHandlers[req.Op]
	if !ok {
		return nil, fmt.Errorf("unknown operation %q", req.Op)
	}
	return operation(backgroundContext(), instance, req)
}

func openRuntime(req request) (json.RawMessage, error) {
	instance, err := stow.Open(req.Options)
	if err != nil {
		return nil, err
	}
	state.Lock()
	defer state.Unlock()
	handle := state.next
	state.next++
	state.instances[handle] = instance
	state.observations[handle] = newObservationTable()
	return marshalResult(openResult{Handle: handle, Capabilities: capabilities(instance.Capabilities())})
}

func capabilities(value stow.Capabilities) capabilitiesResult {
	return capabilitiesResult{
		Backend:             value.Backend,
		MaxBytes:            value.MaxBytes,
		MaxObjects:          value.MaxObjects,
		Persistent:          value.Persistent,
		Multipart:           value.Multipart,
		Upstream:            value.Upstream,
		ConditionalWrites:   value.ConditionalWrites,
		GuardedSaves:        value.GuardedSaves,
		DurableSaveRequests: value.DurableSaveRequests,
		RecoveryHolds:       value.RecoveryHolds,
	}
}

func failure(code, message string) response {
	return response{Version: protocolVersion, Error: &errorPayload{Code: code, Message: message}}
}

// errorCodeTable maps a sentinel to the code a caller branches on. A table, not a
// switch: the save vocabulary grows, and a switch would grow errorResponse's
// complexity one case at a time until the ratchet objected to a new error code.
var errorCodeTable = []struct {
	err  error
	code string
}{
	{stow.ErrQuotaExceeded, "quota_exceeded"},
	{stow.ErrClosed, "closed"},
	{stow.ErrInvalidListLimit, "invalid_list_limit"},
	{stow.ErrUnsupportedBackend, "unsupported_backend"},
	{stow.ErrBucketNotFound, "bucket_not_found"},
	{stow.ErrBucketExists, "bucket_exists"},
	{stow.ErrBucketNotEmpty, "bucket_not_empty"},
	{stow.ErrObjectNotFound, "object_not_found"},
	{stow.ErrInvalidBucket, "invalid_bucket"},
	{stow.ErrInvalidKey, "invalid_key"},
	{stow.ErrPreconditionFailed, "precondition_failed"},
	{stow.ErrConditionalWritesUnsupported, "conditional_write_unsupported"},
	{errUnknownObservation, "invalid_observation"},
	{errObservationExpired, "observation_expired"},
	{stow.ErrGuardedSavesUnsupported, "guarded_saves_unsupported"},
	{stow.ErrSaveConflict, "save_conflict"},
	{stow.ErrInvalidSaveCondition, "invalid_save_condition"},
	{stow.ErrSaveRequestsUnsupported, "save_requests_unsupported"},
	{stow.ErrSaveRequestNotFound, "save_request_not_found"},
	{stow.ErrSaveRequestConflict, "save_request_conflict"},
	{stow.ErrInvalidSaveRequest, "invalid_save_request"},
	{stow.ErrSaveRequestNotCommitted, "save_request_not_committed"},
	{stow.ErrSaveRequestUnknown, "save_request_unknown"},
}

func errorResponse(err error) response {
	for _, entry := range errorCodeTable {
		if errors.Is(err, entry.err) {
			return failure(entry.code, err.Error())
		}
	}
	return failure("runtime_error", err.Error())
}

func encodeResponse(value response) string {
	if value.Version == 0 {
		value.Version = protocolVersion
	}
	data, err := json.Marshal(value)
	if err != nil {
		return `{"version":1,"ok":false,"error":{"code":"response_encoding","message":"response encoding failed"}}`
	}
	return string(data)
}

func marshalResult(value interface{}) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func backgroundContext() context.Context {
	return context.Background()
}
