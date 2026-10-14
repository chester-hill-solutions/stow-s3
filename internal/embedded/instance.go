package embedded

import (
	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	stowruntime "github.com/chester-hill-solutions/stow-s3/internal/runtime"
)

type Authority = authority.Authority
type ErrNotAuthorized = authority.ErrNotAuthorized

// WrapInstance binds native constructors to the same embedded facade.
func WrapInstance(instance *stowruntime.Instance) *Runtime { return &Runtime{inner: instance} }

// UnwrapInstance is for native adapters; it is not part of the public API.
func UnwrapInstance(runtime *Runtime) *stowruntime.Instance { return runtime.inner }

func MapSaveError(err error) error { return mapSaveError(err) }
