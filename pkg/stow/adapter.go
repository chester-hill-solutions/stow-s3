package stow

import (
	"errors"

	"github.com/chester-hill-solutions/stow-s3/internal/adapterbridge"
	"github.com/chester-hill-solutions/stow-s3/internal/embedded"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence"
)

func OpenAdapter(options Options, backend persistence.Adapter, buckets ...string) (*Runtime, error) {
	if options.Store != nil {
		return nil, errors.New("stow: Options.Store and a persistence adapter cannot both be supplied")
	}
	store, err := adapterbridge.Bind(backend, buckets...)
	if err != nil {
		return nil, err
	}
	instance, err := stowruntime.OpenWithStore(stowruntime.Options{
		Backend:             stowruntime.Backend(options.Backend),
		MaxBytes:            options.MaxBytes,
		MaxObjects:          options.MaxObjects,
		MaxMultipartUploads: options.MaxMultipartUploads,
		Authority:           options.Authority,
	}, store, nil)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return embedded.WrapInstance(instance), nil
}
