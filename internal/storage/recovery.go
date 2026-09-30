package storage

import (
	"context"
	"errors"
)

var (
	ErrRecoveryHoldsUnsupported = errors.New("recovery holds unsupported")
	ErrInvalidRecoveryHold      = errors.New("invalid recovery hold")
	ErrRecoveryHoldNotFound     = errors.New("recovery hold not found")
	ErrRecoveryHoldFull         = errors.New("recovery hold capacity exhausted")
	// Declared here rather than with the capacity budget it is charged against:
	// the refusal arrives from a store and must reach the runtime, which may
	// not link the capacity package.
	ErrRecoveryHeld = errors.New("resource required by a recovery operation")
)

type ObjectResource struct {
	Bucket, Key string
	Guard       WriteGuard
}

type RecoveryHoldOptions struct {
	ID, Owner string
	Objects   []ObjectResource
}

type RecoveryHoldStore interface {
	SupportsRecoveryHolds() bool
	BeginRecoveryHold(context.Context, RecoveryHoldOptions) error
	ReleaseRecoveryHold(context.Context, string, string) error
}
