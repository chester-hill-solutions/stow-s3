package embedded

import (
	"errors"
	"time"
)

type Backend string

const (
	BackendMemory     Backend = "memory"
	BackendFilesystem Backend = "filesystem"
	BackendHost       Backend = "host"
	// BackendWorkspace exposes a working directory as real files through OpenWorkspace.
	// Workspace.Capabilities reports it. Open returns ErrUnsupportedBackend because
	// its Options contain no workspace directory.
	BackendWorkspace Backend = "workspace"
)

type Options struct {
	Backend             Backend
	MaxBytes            int64
	MaxObjects          int64
	MaxMultipartUploads int64

	// Store supplies object storage; nil selects the built-in backend. Backend names
	// describe the store but do not establish durability or optional capabilities.
	Store Store

	// Authority is what this environment permits, enforced below every
	// interface so an embedded caller and an S3 client are granted the same
	// things. A nil pointer permits everything, which is what an embedded
	// runtime has always done.
	//
	// To start with nothing and grant as you go, take the address of a value:
	//
	//	readOnly := stow.ReadOnly()
	//	rt, err := stow.Open(stow.Options{Authority: &readOnly})
	Authority *Authority
}

type Capabilities struct {
	Backend             Backend
	MaxBytes            int64
	MaxObjects          int64
	MaxMultipartUploads int64
	Persistent          bool
	Multipart           bool
	Upstream            bool
	ConditionalWrites   bool
	GuardedSaves        bool
	DurableSaveRequests bool
	RecoveryHolds       bool
}

type Usage struct {
	Bytes   int64
	Objects int64
}

type Bucket struct {
	Name         string
	CreationDate time.Time
}

type Object struct {
	Bucket       string
	Key          string
	Data         []byte
	Size         int64
	ETag         string
	ContentType  string
	Metadata     map[string]string
	LastModified time.Time
}

type ObjectPage struct {
	Objects    []Object
	Truncated  bool
	NextCursor string
	// CommonPrefixes are the delimiter groups this page collapsed, in row order.
	CommonPrefixes []string
}

type PutOptions struct {
	ContentType string
	Metadata    map[string]string
	// Conditional writes use existing ETags; IfNoneMatch "*" requires absence.
	// Supplied Stores opt in through ConditionalWriteStore.
	IfMatch     string
	IfNoneMatch string
}

type ListOptions struct {
	Prefix string
	// Delimiter groups keys that share it into CommonPrefixes, which is how one
	// directory level is listed. Empty means a flat listing of every key.
	Delimiter string
	Cursor    string
	Limit     int
}

var (
	ErrClosed             = errors.New("stow: runtime is closed")
	ErrQuotaExceeded      = errors.New("stow: runtime quota exceeded")
	ErrUnsupportedBackend = errors.New("stow: unsupported runtime backend")
	ErrInvalidListLimit   = errors.New("stow: list limit must not be negative")
)
