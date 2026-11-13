package storage

import "context"

type WorkspaceFileSaveRequest struct {
	RequestKey       string
	BasisTokenDigest string
	Write            WorkspaceFileWriteRequest
}

type WorkspaceFileSaveStore interface {
	ReplayWorkspaceFileSave(context.Context, string, WorkspaceFileSaveRequest) (SaveReceipt, bool, error)
	SaveWorkspaceFile(context.Context, string, WorkspaceFileSaveRequest) (SaveReceipt, error)
	ResolveWorkspaceFileSave(context.Context, string, string, string) (SaveReceipt, error)
}
