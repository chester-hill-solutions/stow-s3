package stow

import "github.com/chester-hill-solutions/stow-s3/internal/embedded"

type (
	ObjectRecoveryReference   = embedded.ObjectRecoveryReference
	ObjectRecoveryHoldOptions = embedded.ObjectRecoveryHoldOptions
)

var (
	ErrRecoveryHoldsUnsupported = embedded.ErrRecoveryHoldsUnsupported
	ErrInvalidRecoveryHold      = embedded.ErrInvalidRecoveryHold
)
