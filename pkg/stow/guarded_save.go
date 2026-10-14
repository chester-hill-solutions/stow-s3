package stow

import (
	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
)

type (
	SaveOutcome   = stowruntime.SaveOutcome
	SaveCondition = embedded.SaveCondition
	SaveResult    = embedded.SaveResult
	SaveOptions   = embedded.SaveOptions
)

const (
	SaveCommitted    = embedded.SaveCommitted
	SaveNotCommitted = embedded.SaveNotCommitted
	SaveUnknown      = embedded.SaveUnknown
)

var (
	ErrGuardedSavesUnsupported = embedded.ErrGuardedSavesUnsupported
	ErrInvalidSaveCondition    = embedded.ErrInvalidSaveCondition
	ErrSaveConflict            = embedded.ErrSaveConflict
)

func ReplacementCondition() SaveCondition { return embedded.ReplacementCondition() }
