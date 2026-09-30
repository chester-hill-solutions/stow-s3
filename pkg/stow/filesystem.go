package stow

import (
	"errors"
	"runtime"

	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

type FilesystemOptions struct {
	Dir       string
	Options   Options
	Namespace *CapacityNamespace
}

// OpenFilesystem owns one exclusively locked durable directory; Reset is unavailable.
func OpenFilesystem(options FilesystemOptions) (*Runtime, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, ErrUnsupportedBackend
	}
	if options.Dir == "" || options.Options.Store != nil {
		return nil, errors.New("stow: filesystem directory required and Store must be unset")
	}
	if options.Namespace != nil && options.Namespace.inner == nil {
		return nil, ErrCapacityInvalid
	}
	if options.Options.Backend != "" && options.Options.Backend != BackendFilesystem {
		return nil, ErrUnsupportedBackend
	}
	store, err := openFilesystemStore(options)
	if err != nil {
		return nil, mapSaveError(err)
	}
	instance, err := stowruntime.OpenWithStore(stowruntime.Options{
		Backend:  stowruntime.BackendFilesystem,
		MaxBytes: options.Options.MaxBytes, MaxObjects: options.Options.MaxObjects,
		MaxMultipartUploads: options.Options.MaxMultipartUploads, Authority: options.Options.Authority,
	}, store, nil)
	if err != nil {
		_ = store.Close()
		return nil, mapSaveError(err)
	}
	return &Runtime{inner: instance}, nil
}

// openFilesystemStore binds the namespace as the directory is opened rather than
// afterwards. Opening a bound directory unbound is refused by the store, so
// binding later would fail on every reopen of a store that already had a
// binding: the second open of a directory is exactly the case that has to work.
func openFilesystemStore(options FilesystemOptions) (*fs.FilesystemStore, error) {
	if options.Namespace == nil {
		return fs.NewFilesystemStore(options.Dir)
	}
	return fs.NewFilesystemStoreWithNamespace(options.Dir, options.Namespace.inner)
}
