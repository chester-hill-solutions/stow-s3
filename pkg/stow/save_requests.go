package stow

import "github.com/chester-hill-solutions/stow-s3/internal/embedded"

var (
	ErrSaveRequestsUnsupported = embedded.ErrSaveRequestsUnsupported
	ErrInvalidSaveRequest      = embedded.ErrInvalidSaveRequest
	ErrSaveRequestConflict     = embedded.ErrSaveRequestConflict
	ErrSaveRequestNotFound     = embedded.ErrSaveRequestNotFound
	ErrSaveRequestNotCommitted = embedded.ErrSaveRequestNotCommitted
	ErrSaveRequestUnknown      = embedded.ErrSaveRequestUnknown
	ErrSaveRequestFull         = embedded.ErrSaveRequestFull
)
