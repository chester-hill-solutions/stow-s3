package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func hostCheckConditions(_ js.Value, args []js.Value) interface{} {
	var value response
	if len(args) != 1 || args[0].Type() != js.TypeString {
		value = failure("invalid_request", "conditions require JSON metadata")
	} else {
		value = evaluateHostConditions(args[0].String())
	}
	value.Version = binaryProtocolVersion
	return encodeResponse(value)
}

func evaluateHostConditions(encoded string) response {
	var conditions struct {
		IfMatch     string `json:"ifMatch"`
		IfNoneMatch string `json:"ifNoneMatch"`
		Existing    *struct {
			ETag string `json:"etag"`
		} `json:"existing"`
	}
	if err := json.Unmarshal([]byte(encoded), &conditions); err != nil {
		return failure("invalid_request", err.Error())
	}
	var existing *storage.ObjectMeta
	if conditions.Existing != nil {
		existing = &storage.ObjectMeta{ETag: conditions.Existing.ETag}
	}
	if err := storage.CheckWritePreconditions(storage.PutOptions{IfMatch: conditions.IfMatch, IfNoneMatch: conditions.IfNoneMatch}, existing); err != nil {
		return failure("precondition_failed", err.Error())
	}
	return response{OK: true}
}
