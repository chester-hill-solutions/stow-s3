package storage

import (
	"context"
	"errors"
)

var (
	ErrWorkspaceFileConflict = errors.New("workspace file observation conflicts with current path")
	ErrWorkspaceFileBlocked  = errors.New("workspace file publication requires recovery")
)

type WorkspaceFileWriteRequest struct {
	Path         string
	Data         []byte
	Basis        *WorkspaceFileObservation
	Replace      bool
	MaxFileBytes int64
}

type WorkspaceFileOutcome string

const (
	WorkspaceFileNotCommitted WorkspaceFileOutcome = "not_committed"
	WorkspaceFileCommitted    WorkspaceFileOutcome = "committed"
	WorkspaceFileUnknown      WorkspaceFileOutcome = "unknown"
)

type WorkspaceFilePublication struct {
	Outcome    WorkspaceFileOutcome
	Meta       *ObjectMeta
	IndexReady bool
}

type WorkspaceFileWriteStore interface {
	PublishWorkspaceFile(context.Context, string, WorkspaceFileWriteRequest) (WorkspaceFilePublication, error)
}

func CheckWorkspaceFileBasis(expected, current WorkspaceFileObservation) error {
	if expected.Path != current.Path || expected.Absent != current.Absent || current.Identity == nil || !current.Identity.Same(expected.Identity) {
		return ErrWorkspaceFileConflict
	}
	if expected.Absent {
		return nil
	}
	if expected.Digest != current.Digest || expected.Size != current.Size || expected.Mode != current.Mode || !expected.Modified.Equal(current.Modified) || expected.Generation != current.Generation || expected.Metadata != current.Metadata {
		return ErrWorkspaceFileConflict
	}
	return nil
}
