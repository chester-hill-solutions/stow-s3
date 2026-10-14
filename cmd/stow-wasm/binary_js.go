package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"syscall/js"
)

const binaryProtocolVersion = 2

var binaryReadBuffers sync.Pool

func parseBinaryRequest(args []js.Value) (request, error) {
	if len(args) != 2 || args[0].Type() != js.TypeString || !args[1].InstanceOf(js.Global().Get("Uint8Array")) {
		return request{}, fmt.Errorf("expected JSON metadata and Uint8Array payload")
	}
	var req request
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		return request{}, fmt.Errorf("invalid JSON request: %w", err)
	}
	if req.Version != binaryProtocolVersion {
		return request{}, fmt.Errorf("unsupported protocol version %d", req.Version)
	}
	if req.Data != "" {
		return request{}, fmt.Errorf("binary metadata must not contain encoded object data")
	}
	req.binaryData = make([]byte, args[1].Length())
	js.CopyBytesToGo(req.binaryData, args[1])
	return req, nil
}

func callBinary(_ js.Value, args []js.Value) interface{} {
	req, err := parseBinaryRequest(args)
	if err != nil {
		return binaryResponse(failure("invalid_request", err.Error()), nil)
	}
	return dispatchBinary(req)
}

func dispatchBinary(req request) js.Value {
	var output []byte
	var buffer *[]byte
	req.binaryOutput = &output
	req.binaryBuffer = &buffer
	defer func() {
		if buffer != nil {
			binaryReadBuffers.Put(buffer)
		}
	}()
	result, err := dispatch(req)
	if err != nil {
		return binaryResponse(errorResponse(err), nil)
	}
	return binaryResponse(response{OK: true, Result: result}, output)
}

func binaryResponse(value response, data []byte) js.Value {
	value.Version = binaryProtocolVersion
	result := js.Global().Get("Object").New()
	result.Set("metadata", encodeResponse(value))
	if data != nil {
		payload := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(payload, data)
		result.Set("data", payload)
	}
	return result
}
