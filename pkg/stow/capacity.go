package stow

import (
	"context"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type CapacityHostOptions = capacity.HostOptions
type CapacityNamespaceOptions = capacity.NamespaceOptions
type CapacitySnapshot = capacity.Snapshot

var (
	ErrCapacityFull     = capacity.ErrFull
	ErrCapacityInvalid  = capacity.ErrInvalid
	ErrCapacityConflict = capacity.ErrConflict
	ErrCapacityUnknown  = capacity.ErrUnknown
	// One refusal whichever side raised it: the value a namespace reports for a
	// pinned dependency is the value a store reports for a held object.
	ErrRecoveryHeld = storage.ErrRecoveryHeld
)

// CapacityHost configures trusted namespace budgets; it does not authenticate callers.
type CapacityHost struct{ inner *capacity.Host }
type CapacityNamespace struct{ inner *capacity.Namespace }

func OpenCapacityHost(options CapacityHostOptions) (*CapacityHost, error) {
	host, err := capacity.Open(options)
	if err != nil {
		return nil, err
	}
	return &CapacityHost{inner: host}, nil
}

func (h *CapacityHost) BindNamespace(options CapacityNamespaceOptions) (*CapacityNamespace, error) {
	if h == nil || h.inner == nil {
		return nil, ErrCapacityInvalid
	}
	namespace, err := h.inner.Bind(options)
	if err != nil {
		return nil, err
	}
	return &CapacityNamespace{inner: namespace}, nil
}

func (n *CapacityNamespace) Snapshot(ctx context.Context) (CapacitySnapshot, error) {
	if n == nil || n.inner == nil {
		return CapacitySnapshot{}, ErrCapacityInvalid
	}
	return n.inner.Snapshot(ctx)
}
