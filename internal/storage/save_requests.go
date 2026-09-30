package storage

import (
	"context"
	"errors"
)

var (
	ErrSaveRequestsUnsupported = errors.New("durable save requests unsupported")
	ErrInvalidSaveRequest      = errors.New("invalid save request")
	ErrSaveRequestConflict     = errors.New("save request meaning conflicts with retained request")
	ErrSaveRequestNotFound     = errors.New("save request not found")
	ErrSaveRequestNotCommitted = errors.New("save request did not commit")
	ErrSaveRequestUnknown      = errors.New("save request effect unknown")
	ErrSaveRequestFull         = errors.New("save request journal full")
)

type SaveReceipt struct {
	Meta     *ObjectMeta
	Outcome  string
	Replayed bool
}

type SaveRequestStore interface {
	SupportsDurableSaveRequests() bool
	ReplaySaveRequest(ctx context.Context, bucket, key string, data []byte, opts PutOptions) (SaveReceipt, bool, error)
	ResolveSaveRequest(ctx context.Context, bucket, key, requestKey string) (SaveReceipt, error)
}

func CheckWriteGuard(guard *WriteGuard, meta *ObjectMeta, data []byte) error {
	if guard == nil {
		return nil
	}
	if guard.Absent && meta == nil {
		return nil
	}
	if !guard.Absent && meta != nil && guard.Fingerprint == ObjectFingerprint(*meta, data) {
		return nil
	}
	return ErrSaveConflict
}
