package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"
)

func callBinaryAsync(_ js.Value, args []js.Value) interface{} {
	req, err := parseBinaryRequest(args)
	return asyncResponse(func() js.Value {
		if err != nil {
			return binaryResponse(failure("invalid_request", err.Error()), nil)
		}
		req.async = true
		return dispatchBinary(req)
	})
}

func openHost(_ js.Value, args []js.Value) interface{} {
	req, host, err := parseHostRequest(args)
	return asyncResponse(func() js.Value {
		if err != nil {
			return binaryResponse(failure("invalid_request", err.Error()), nil)
		}
		result, openErr := openHostRuntime(req, host)
		if openErr != nil {
			return binaryResponse(errorResponse(openErr), nil)
		}
		return binaryResponse(response{OK: true, Result: result}, nil)
	})
}

func parseHostRequest(args []js.Value) (request, js.Value, error) {
	if len(args) != 2 || args[0].Type() != js.TypeString || args[1].Type() != js.TypeObject {
		return request{}, js.Undefined(), fmt.Errorf("expected JSON metadata and host store")
	}
	var req request
	if err := json.Unmarshal([]byte(args[0].String()), &req); err != nil {
		return request{}, js.Undefined(), err
	}
	if req.Version != binaryProtocolVersion || req.Op != "open" {
		return request{}, js.Undefined(), fmt.Errorf("expected binary protocol open request")
	}
	if args[1].Get("call").Type() != js.TypeFunction {
		return request{}, js.Undefined(), fmt.Errorf("host store must provide call")
	}
	req.async = true
	return req, args[1], nil
}

func asyncResponse(run func() js.Value) js.Value {
	executor := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		resolve := args[0]
		go func() { resolve.Invoke(run()) }()
		return nil
	})
	promise := js.Global().Get("Promise").New(executor)
	executor.Release()
	return promise
}
