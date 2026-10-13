//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func handleGetObjectRange(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	if req.Range.Length > hostChunkLimit {
		return nil, fmt.Errorf("binary object range exceeds %d bytes", hostChunkLimit)
	}
	reader, object, err := instance.OpenObjectRange(ctx, req.Bucket, req.Key, req.Range)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	object.Data, err = io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return marshalObjectResult(object, req)
}
