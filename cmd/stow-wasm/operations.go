//go:build js && wasm

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

var operationHandlers = map[string]handler{
	"createBucket":      handleCreateBucket,
	"deleteBucket":      handleDeleteBucket,
	"listBuckets":       handleListBuckets,
	"putObject":         handlePutObject,
	"getObject":         handleGetObject,
	"getObjectRange":    handleGetObjectRange,
	"openObjectRead":    handleOpenObjectRead,
	"readObjectChunk":   handleReadObjectChunk,
	"closeObjectRead":   handleCloseObjectRead,
	"openObjectWrite":   handleOpenObjectWrite,
	"writeObjectChunk":  handleWriteObjectChunk,
	"commitObjectWrite": handleCommitObjectWrite,
	"abortObjectWrite":  handleAbortObjectWrite,
	"headObject":        handleHeadObject,
	"listObjects":       handleListObjects,
	"deleteObject":      handleDeleteObject,
	"copyObject":        handleCopyObject,
	"reset":             handleReset,
	"close":             handleClose,
	"usage":             handleUsage,
	"capabilities":      handleCapabilities,
}

func handleCreateBucket(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	return nil, instance.CreateBucket(ctx, req.Bucket)
}

func handleDeleteBucket(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	return nil, instance.DeleteBucket(ctx, req.Bucket)
}

func handleListBuckets(ctx context.Context, instance *stow.Runtime, _ request) (json.RawMessage, error) {
	buckets, err := instance.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	result := bucketListResult{Buckets: make([]bucketResult, 0, len(buckets))}
	for _, bucket := range buckets {
		result.Buckets = append(result.Buckets, bucketResult{
			Name:         bucket.Name,
			CreationDate: bucket.CreationDate.UTC().Format("2006-01-02T15:04:05.000Z"),
		})
	}
	return marshalResult(result)
}

func handlePutObject(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	data := req.binaryData
	if req.binaryOutput == nil {
		var err error
		data, err = base64.StdEncoding.DecodeString(req.Data)
		if err != nil {
			return nil, fmt.Errorf("decode object data: %w", err)
		}
	}
	object, err := instance.PutObject(ctx, req.Bucket, req.Key, data, stow.PutOptions{
		ContentType: req.ContentType,
		Metadata:    req.Metadata,
		IfMatch:     req.IfMatch,
		IfNoneMatch: req.IfNoneMatch,
	})
	if err != nil {
		return nil, err
	}
	return marshalObjectResult(object, req)
}

func handleGetObject(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	object, err := instance.GetObject(ctx, req.Bucket, req.Key)
	if err != nil {
		return nil, err
	}
	return marshalObjectResult(object, req)
}

func handleHeadObject(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	object, err := instance.HeadObject(ctx, req.Bucket, req.Key)
	if err != nil {
		return nil, err
	}
	return marshalObjectResult(object, req)
}

func handleListObjects(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	page, err := instance.ListObjects(ctx, req.Bucket, req.List)
	if err != nil {
		return nil, err
	}
	result := listResult{
		Objects:    make([]objectResult, 0, len(page.Objects)),
		Truncated:  page.Truncated,
		NextCursor: page.NextCursor,
	}
	for _, object := range page.Objects {
		result.Objects = append(result.Objects, objectResultOf(object))
	}
	return marshalResult(result)
}

func handleDeleteObject(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	return nil, instance.DeleteObject(ctx, req.Bucket, req.Key)
}

func handleCopyObject(ctx context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	sourceBucket := req.SourceBucket
	if sourceBucket == "" {
		sourceBucket = req.Bucket
	}
	sourceKey := req.SourceKey
	if sourceKey == "" {
		sourceKey = req.Key
	}
	object, err := instance.CopyObject(ctx, sourceBucket, sourceKey, req.DestinationBucket, req.DestinationKey)
	if err != nil {
		return nil, err
	}
	return marshalObjectResult(object, req)
}

func handleReset(ctx context.Context, instance *stow.Runtime, _ request) (json.RawMessage, error) {
	return nil, instance.Reset(ctx)
}

func handleClose(_ context.Context, instance *stow.Runtime, req request) (json.RawMessage, error) {
	closeRuntimeStreams(req.Handle)
	err := instance.Close()
	state.Lock()
	delete(state.instances, req.Handle)
	state.Unlock()
	releaseRuntimeStreamHandle(req.Handle)
	return nil, err
}

func handleUsage(_ context.Context, instance *stow.Runtime, _ request) (json.RawMessage, error) {
	usage := instance.Usage()
	return marshalResult(usageResult{Bytes: usage.Bytes, Objects: usage.Objects})
}

func handleCapabilities(_ context.Context, instance *stow.Runtime, _ request) (json.RawMessage, error) {
	return marshalResult(capabilities(instance.Capabilities()))
}

func objectResultOf(object stow.Object) objectResult {
	return objectResult{
		Bucket:       object.Bucket,
		Key:          object.Key,
		Data:         base64.StdEncoding.EncodeToString(object.Data),
		Size:         object.Size,
		ETag:         object.ETag,
		ContentType:  object.ContentType,
		Metadata:     object.Metadata,
		LastModified: object.LastModified.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
}

func marshalObjectResult(object stow.Object, req request) (json.RawMessage, error) {
	if req.binaryOutput == nil {
		return marshalResult(objectResultOf(object))
	}
	*req.binaryOutput = object.Data
	if req.Op == "getObject" && object.Data == nil {
		*req.binaryOutput = []byte{}
	}
	object.Data = nil
	return marshalResult(objectResultOf(object))
}
