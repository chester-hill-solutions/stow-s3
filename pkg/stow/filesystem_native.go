//go:build linux || darwin

package stow

import (
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
)

// OpenFilesystem owns one exclusively locked durable directory; Reset is unavailable.
func OpenFilesystem(options FilesystemOptions) (*Runtime, error) {
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
		return nil, embedded.MapSaveError(err)
	}
	instance, err := stowruntime.OpenWithStore(stowruntime.Options{
		Backend:  stowruntime.BackendFilesystem,
		MaxBytes: options.Options.MaxBytes, MaxObjects: options.Options.MaxObjects,
		MaxMultipartUploads: options.Options.MaxMultipartUploads, Authority: options.Options.Authority,
	}, store, nil)
	if err != nil {
		_ = store.Close()
		return nil, embedded.MapSaveError(err)
	}
	return embedded.WrapInstance(instance), nil
}

// Bind while opening: a previously bound directory refuses an unbound reopen,
// so opening first and binding afterward fails on that directory.
func openFilesystemStore(options FilesystemOptions) (*fs.FilesystemStore, error) {
	if options.Namespace == nil {
		return fs.NewFilesystemStore(options.Dir)
	}
	return fs.NewFilesystemStoreWithNamespace(options.Dir, options.Namespace.inner)
}
