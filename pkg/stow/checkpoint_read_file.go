package stow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (r *CheckpointReader) ReadFile(ctx context.Context, path string) (CheckpointReadResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	opCtx, cancel, err := r.operationContext(ctx)
	if err != nil {
		return CheckpointReadResult{}, err
	}
	defer cancel()
	file, found := r.files[path]
	if !found || !validWorkspaceReadPath(path, false) || workspaceReadExcluded(path) || !workspaceReadFileAllowed(path, r.readRoots) {
		return CheckpointReadResult{}, ErrCheckpointReadUnavailable
	}
	if file.Size > checkpointReadMaxFileBytes {
		return CheckpointReadResult{}, ErrCheckpointReadTooLarge
	}
	return r.readFileLocked(opCtx, path, file, 0, file.Size)
}

func (r *CheckpointReader) ReadRange(ctx context.Context, path string, offset, length int64) (CheckpointReadResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	opCtx, cancel, err := r.operationContext(ctx)
	if err != nil {
		return CheckpointReadResult{}, err
	}
	defer cancel()
	file, found := r.files[path]
	if !found || !validWorkspaceReadPath(path, false) || workspaceReadExcluded(path) || !workspaceReadFileAllowed(path, r.readRoots) ||
		offset < 0 || length < 1 || length > checkpointReadMaxFileBytes || offset > file.Size {
		return CheckpointReadResult{}, ErrCheckpointReadUnavailable
	}
	if file.Size > r.maxVerify {
		return CheckpointReadResult{}, ErrCheckpointReadTooLarge
	}
	count := min(length, file.Size-offset)
	return r.readFileLocked(opCtx, path, file, offset, count)
}

func (r *CheckpointReader) readFileLocked(ctx context.Context, path string, expected CheckpointFile, offset, count int64) (CheckpointReadResult, error) {
	if err := ctx.Err(); err != nil {
		return CheckpointReadResult{}, r.operationError(ctx, err)
	}
	if expected.Size > r.maxVerify {
		return CheckpointReadResult{}, ErrCheckpointReadTooLarge
	}
	if err := r.verifyManifest(ctx); err != nil {
		return CheckpointReadResult{}, r.operationError(ctx, err)
	}
	file, before, err := r.openCheckpointPayload(path, expected)
	if err != nil {
		return CheckpointReadResult{}, err
	}
	defer file.Close()
	data, digest, size, err := hashCheckpointPayload(ctx, file, offset, count, expected.Size)
	if err != nil {
		return CheckpointReadResult{}, r.operationError(ctx, err)
	}
	if err := r.verifyCheckpointPayload(file, before, expected, digest, size); err != nil {
		return CheckpointReadResult{}, ErrCheckpointReadUnavailable
	}
	if err := r.verifyManifest(ctx); err != nil {
		return CheckpointReadResult{}, r.operationError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return CheckpointReadResult{}, r.operationError(ctx, err)
	}
	return CheckpointReadResult{Path: path, Data: data, Offset: offset, Size: expected.Size, SHA256: digest}, nil
}

func (r *CheckpointReader) openCheckpointPayload(path string, expected CheckpointFile) (*os.File, os.FileInfo, error) {
	if r.root.Check() != nil {
		return nil, nil, ErrCheckpointReadUnavailable
	}
	file, err := r.root.OpenRegularFile(filepath.Join("files", filepath.FromSlash(path)))
	if err != nil {
		return nil, nil, ErrCheckpointReadUnavailable
	}
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() != expected.Size || !checkpointStoredMode(before.Mode(), expected.Mode) {
		_ = file.Close()
		return nil, nil, ErrCheckpointReadUnavailable
	}
	return file, before, nil
}

func (r *CheckpointReader) verifyCheckpointPayload(file *os.File, before os.FileInfo, expected CheckpointFile, digest string, size int) error {
	after, err := file.Stat()
	if err != nil || !sameCheckpointFile(before, after) || int64(size) != expected.Size ||
		!strings.EqualFold(digest, expected.SHA256) || r.root.Check() != nil {
		return ErrCheckpointReadUnavailable
	}
	return nil
}

func hashCheckpointPayload(ctx context.Context, file io.Reader, offset, count, expectedSize int64) ([]byte, string, int, error) {
	data := make([]byte, int(count))
	windowEnd := offset + count
	buffer := make([]byte, 64<<10)
	hasher := sha256.New()
	position := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return nil, "", 0, err
		}
		remaining := expectedSize - position
		readSize := min(int64(len(buffer)), remaining+1)
		read, readErr := file.Read(buffer[:readSize])
		if read > 0 {
			validRead := min(int64(read), remaining)
			if _, err := hasher.Write(buffer[:validRead]); err != nil {
				return nil, "", 0, err
			}
			chunkEnd := position + validRead
			copyStart := max(position, offset)
			copyEnd := min(chunkEnd, windowEnd)
			if copyEnd > copyStart {
				start := copyStart - position
				end := copyEnd - position
				copy(data[copyStart-offset:copyEnd-offset], buffer[start:end])
			}
			position = chunkEnd
			if int64(read) > validRead {
				return nil, "", 0, ErrCheckpointReadUnavailable
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, "", 0, ErrCheckpointReadUnavailable
		}
		if read == 0 {
			continue
		}
	}
	if position != expectedSize {
		return nil, "", 0, ErrCheckpointReadUnavailable
	}
	return data, hex.EncodeToString(hasher.Sum(nil)), int(position), nil
}
