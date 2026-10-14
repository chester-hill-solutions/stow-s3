package stow

import "github.com/chester-hill-solutions/stow-s3/internal/embedded"

type (
	Backend      = embedded.Backend
	Options      = embedded.Options
	Capabilities = embedded.Capabilities
	Usage        = embedded.Usage
	Bucket       = embedded.Bucket
	Object       = embedded.Object
	ObjectPage   = embedded.ObjectPage
	PutOptions   = embedded.PutOptions
	ListOptions  = embedded.ListOptions
)

const (
	BackendMemory     = embedded.BackendMemory
	BackendFilesystem = embedded.BackendFilesystem
	BackendHost       = embedded.BackendHost
	BackendWorkspace  = embedded.BackendWorkspace
)

var (
	ErrClosed             = embedded.ErrClosed
	ErrQuotaExceeded      = embedded.ErrQuotaExceeded
	ErrUnsupportedBackend = embedded.ErrUnsupportedBackend
	ErrInvalidListLimit   = embedded.ErrInvalidListLimit
)
