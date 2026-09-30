package stow

import (
	"context"
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

var (
	ErrSaveRequestsUnsupported = errors.New("stow: durable save requests unsupported")
	ErrInvalidSaveRequest      = errors.New("stow: invalid save request")
	ErrSaveRequestConflict     = errors.New("stow: save request key has different meaning")
	ErrSaveRequestNotFound     = errors.New("stow: save request receipt not found")
	ErrSaveRequestNotCommitted = errors.New("stow: save request was not committed")
	ErrSaveRequestUnknown      = errors.New("stow: save request effect unknown")
	ErrSaveRequestFull         = errors.New("stow: save request retention full")
)

func (r *Runtime) SupportsDurableSaveRequests() bool { return r.inner.SupportsDurableSaveRequests() }

// ResolveSave discloses a retained result under current read authority; absence does not prove no effect.
func (r *Runtime) ResolveSave(ctx context.Context, bucket, key, requestKey string) (SaveResult, error) {
	result, err := r.inner.ResolveSave(ctx, bucket, key, requestKey)
	return SaveResult{Object: objectOf(result.Object), Outcome: result.Outcome, Replayed: result.Replayed}, mapSaveError(err)
}

func mapSaveRequestError(err error) error {
	for _, pair := range [][2]error{
		{storage.ErrSaveRequestsUnsupported, ErrSaveRequestsUnsupported},
		{storage.ErrInvalidSaveRequest, ErrInvalidSaveRequest},
		{storage.ErrSaveRequestConflict, ErrSaveRequestConflict},
		{storage.ErrSaveRequestNotFound, ErrSaveRequestNotFound},
		{storage.ErrSaveRequestNotCommitted, ErrSaveRequestNotCommitted},
		{storage.ErrSaveRequestUnknown, ErrSaveRequestUnknown},
		{storage.ErrSaveRequestFull, ErrSaveRequestFull},
	} {
		if errors.Is(err, pair[0]) {
			return pair[1]
		}
	}
	return mapError(err)
}
