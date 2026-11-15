package storage

import (
	"context"
	"errors"
	"time"
)

var (
	ErrWorkspacePathSpelling  = errors.New("workspace path spelling collision")
	ErrWorkspaceParentInvalid = errors.New("workspace parent is unavailable")
)

type WorkspaceFileObservation struct {
	Path       string
	Data       []byte
	Absent     bool
	Digest     string
	Mode       uint32
	Modified   time.Time
	Generation string
	Identity   WorkspaceFileIdentity
	Size       int64
	Metadata   string
}

type WorkspaceFileIdentity interface {
	Same(WorkspaceFileIdentity) bool
}

type WorkspaceFileReadStore interface {
	WorkspaceFileBucket() string
	ObserveWorkspaceFile(context.Context, string, string, int64) (WorkspaceFileObservation, error)
}
