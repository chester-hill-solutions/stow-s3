package fs

import (
	"bytes"
	"context"
	"io"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func (s *FilesystemStore) stageBody(ctx context.Context, body io.Reader, p *filesystemStagedObject) (storage.StagedObject, error) {
	if body == nil {
		body = bytes.NewReader(nil)
	}
	prefix, complete, err := readStagePrefix(ctx, body)
	if complete {
		prepared, err := storage.PreparePut(storage.NewOwnedBody(prefix), p.options)
		if err != nil {
			_ = p.Abort(context.Background())
			return nil, err
		}
		p.prepared = &prepared
		return p, nil
	}
	if err != nil {
		_ = p.Abort(context.Background())
		return nil, err
	}
	return s.stageFile(ctx, io.MultiReader(bytes.NewReader(prefix), body), p)
}

func readStagePrefix(ctx context.Context, body io.Reader) ([]byte, bool, error) {
	prefix := make([]byte, stagePrefixSize(body))
	reader := &stageContextReader{context: ctx, reader: body}
	n, err := io.ReadFull(reader, prefix)
	if reader.err != nil && reader.err != io.EOF {
		return nil, false, reader.err
	}
	if reader.err == io.EOF {
		return prefix[:n], true, nil
	}
	return prefix[:n], false, err
}

func stagePrefixSize(body io.Reader) int {
	hint := int64(64 << 10)
	if sized, ok := body.(interface{ ReadSize() int64 }); ok {
		hint = sized.ReadSize()
	} else if sized, ok := body.(interface{ Len() int }); ok {
		hint = int64(sized.Len())
	}
	if hint < 0 || hint > 64<<10 {
		hint = 64 << 10
	}
	return int(hint) + 1
}

type stageContextReader struct {
	context context.Context
	reader  io.Reader
	err     error
}

func (r *stageContextReader) Read(data []byte) (int, error) {
	if r.context != nil && r.context.Err() != nil {
		r.err = r.context.Err()
		return 0, r.err
	}
	n, err := r.reader.Read(data)
	r.err = err
	return n, err
}
