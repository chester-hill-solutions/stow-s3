package capacity

import (
	"errors"
	"sync"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

const (
	Payload       = "payload"
	Recovery      = "recovery"
	maxStateBytes = 8 << 20
	maxResources  = 4096
)

var (
	ErrFull     = errors.New("namespace capacity exhausted")
	ErrInvalid  = errors.New("invalid namespace capacity request")
	ErrConflict = errors.New("namespace binding or operation conflict")
	ErrUnknown  = errors.New("namespace admission requires verified reconciliation")
	// ErrHeld is the storage-level refusal: a dependency is pinned by work that
	// has not finished with it. It is the same value a store returns when a
	// recovery hold blocks a mutation, so one errors.Is answers both.
	ErrHeld = storage.ErrRecoveryHeld
)

type HostOptions struct {
	Dir           string
	MaxBytes      int64
	MaxNamespaces int
}
type NamespaceOptions struct {
	ID                             string
	MaxBytes, RecoveryReserveBytes int64
	MaxOperations, MaxDependencies int
}
type Resource struct {
	StoreID string `json:"store_id,omitempty"`
	ID      string `json:"id"`
	Version string `json:"version"`
	Bytes   int64  `json:"bytes"`
	Class   string `json:"class"`
}
type Usage struct {
	StoreID   string
	Resources []Resource
}
type Admission struct {
	Usage                       Usage
	StagingBytes, RecoveryBytes int64
}
type Operation struct {
	ID           string     `json:"id"`
	Owner        string     `json:"owner"`
	Meaning      string     `json:"meaning"`
	Dependencies []Resource `json:"dependencies"`
	ReserveBytes int64      `json:"reserve_bytes"`
}
type Snapshot struct {
	PayloadBytes, RecoveryBytes, ReservedRecoveryBytes, PendingBytes int64
	Operations                                                       int
}
type Host struct {
	mu      sync.Mutex
	dir     string
	options HostOptions
}
type Namespace struct {
	host *Host
	id   string
}

type storeState struct {
	Root      string          `json:"root"`
	Resources []Resource      `json:"resources"`
	Pending   *AdmissionState `json:"pending,omitempty"`
}
type AdmissionState struct {
	Resources     []Resource `json:"resources"`
	StagingBytes  int64      `json:"staging_bytes"`
	RecoveryBytes int64      `json:"recovery_bytes"`
}
type operationState struct {
	Operation
	Released bool `json:"released"`
}
type namespaceState struct {
	Options    NamespaceOptions          `json:"options"`
	Stores     map[string]*storeState    `json:"stores"`
	Operations map[string]operationState `json:"operations"`
}
type hostState struct {
	Version       int                        `json:"version"`
	Integrity     string                     `json:"integrity"`
	MaxBytes      int64                      `json:"max_bytes"`
	MaxNamespaces int                        `json:"max_namespaces"`
	Namespaces    map[string]*namespaceState `json:"namespaces"`
}
