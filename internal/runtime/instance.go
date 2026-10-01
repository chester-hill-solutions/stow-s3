package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type Instance struct {
	mu               sync.Mutex
	store            storage.Store
	multipartStore   storage.MultipartStore
	resetStore       func() (storage.Store, error)
	options          Options
	authority        authority.Authority
	policy           *policy.Set
	policyErr        error
	usage            Usage
	multipart        map[string]multipartUsage
	multipartTargets map[string]int
	reservedTargets  map[string]struct{}
	reservedObjects  int64
	reservedBytes    int64
	persistent       bool
	closed           bool
	closeOnce        sync.Once
	closeErr         error
}

type multipartUsage struct {
	upload storage.MultipartUpload
	parts  map[int]int64
}

func Open(options Options) (*Instance, error) {
	normalized, err := normalizeOptions(options, false)
	if err != nil {
		return nil, err
	}
	instance := newInstance(normalized, storage.NewMemoryStore(), func() (storage.Store, error) {
		return storage.NewMemoryStore(), nil
	}, false)
	// Initialized for the same reason OpenWithStore initializes, and the
	// asymmetry was a landmine rather than a live defect: normalizeOptions
	// above rejects every backend but memory, and the memory store is built
	// here, so this walk currently finds no buckets and reconciles nothing.
	// It matters on the day Open accepts a store that can arrive populated --
	// a filesystem backend, or anything reusing a data directory. Skipping it
	// would start usage at zero against objects that already exist, which
	// makes MaxBytes and MaxObjects unenforceable rather than merely
	// unenforced, and it would skip the open-time ErrQuotaExceeded check that
	// decides whether an over-quota store may be opened at all.
	if err := instance.initialize(context.Background()); err != nil {
		return nil, err
	}
	return instance, nil
}

func newInstance(options Options, store storage.Store, resetStore func() (storage.Store, error), persistent bool) *Instance {
	granted := authority.All()
	if options.Authority != nil {
		granted = *options.Authority
	}
	// Validated here so the answer cannot depend on which operation asked, and
	// held as an error rather than discarded so a widening policy refuses.
	var narrow *policy.Set
	var policyErr error
	if options.Policy != nil {
		validated, err := policy.New(*options.Policy, granted)
		if err != nil {
			policyErr = err
		} else {
			narrow = &validated
		}
	}
	multipart, _ := store.(storage.MultipartStore)
	return &Instance{
		store:            store,
		multipartStore:   multipart,
		resetStore:       resetStore,
		options:          options,
		authority:        granted,
		policy:           narrow,
		policyErr:        policyErr,
		multipart:        make(map[string]multipartUsage),
		multipartTargets: make(map[string]int),
		reservedTargets:  make(map[string]struct{}),
		persistent:       persistent,
	}
}

// checkUpload is checkResource for an operation addressed by upload ID.
//
// The resource comes from this instance's own record rather than from the store,
// because the store's answer would have to be read before authorization to know
// what to authorize, and authorization comes first. An upload this instance
// cannot resolve therefore has no established resource, which without a policy
// is the old behaviour and with one is refused: a policy that cannot be
// evaluated must not fall back to allow, or the handle-based operations become
// the one path around every selector.
//
// Reads i.multipart, so it must be called under i.mu.
func (i *Instance) checkUpload(op authority.Operation, uploadID string) error {
	if i.policy == nil && i.policyErr == nil {
		return i.authority.Check(op)
	}
	usage, ok := i.multipart[uploadID]
	if !ok {
		return fmt.Errorf("%w: upload %q", ErrResourceUnresolved, uploadID)
	}
	return i.checkResource(op, policy.Object(usage.upload.Bucket, usage.upload.Key))
}

// check is the authorization point for operations that name neither a resource nor
// a collection: the environment's own lifecycle, and listing the namespace.
//
// It consults the policy twice over, and both are the fix for a deny that was written
// and did not apply. A policy that failed validation is refused here too, not only on
// the resource path, because a widening policy means the author believed a permission
// was in force and it is not. And an entry that withholds the operation is honoured,
// because Reset names no bucket for a selector to match.
func (i *Instance) check(op authority.Operation) error {
	if i.policyErr != nil {
		return i.policyErr
	}
	return i.checkGranted(op, "")
}

// checkCollection is check for an operation that names a collection but no
// resource, so a deny is scoped to that collection. See policy.Set.Denies.
func (i *Instance) checkCollection(op authority.Operation, collection string) error {
	if i.policyErr != nil {
		return i.policyErr
	}
	return i.checkGranted(op, collection)
}

// checkGranted is the answer both of the above give: the environment's, unless a
// policy entry withholds the operation. A deny is all a policy can say here.
func (i *Instance) checkGranted(op authority.Operation, collection string) error {
	if err := i.authority.Check(op); err != nil {
		return err
	}
	if i.policy == nil || !i.policy.Denies(policy.LocalNamespace, collection, op) {
		return nil
	}
	return &authority.ErrNotAuthorized{Operation: op, Authority: i.authority.Without(op)}
}

// checkResource is check for an operation on a named resource, and it is where a
// policy is consulted. A nil policy means no policy, so this is check and the
// answer is unchanged. A policy that failed validation refuses here rather than
// being skipped, because the author wrote one believing it was in force and the
// alternative is silently answering with the wider set.
func (i *Instance) checkResource(op authority.Operation, res policy.Resource) error {
	if i.policyErr != nil {
		return i.policyErr
	}
	if i.policy == nil {
		return i.authority.Check(op)
	}
	return i.policy.Allows(i.authority, res, op)
}

// Authority reports what this environment permits, so an interface can narrow
// its own behaviour to match rather than deciding separately.
func (i *Instance) Authority() authority.Authority {
	return i.authority
}

func (i *Instance) checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (i *Instance) checkOpen() error {
	if i.closed {
		return ErrClosed
	}
	return nil
}

func (i *Instance) capabilitiesLocked() Capabilities {
	return Capabilities{
		Backend:             i.options.Backend,
		MaxBytes:            i.options.MaxBytes,
		MaxObjects:          i.options.MaxObjects,
		MaxMultipartUploads: i.options.MaxMultipartUploads,
		Persistent:          i.persistent,
		Multipart:           i.multipartStore != nil,
	}
}

func (i *Instance) Capabilities() Capabilities {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.capabilitiesLocked()
}

func (i *Instance) Usage() Usage {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.usage
}

func (i *Instance) Reset(ctx context.Context) error {
	if err := i.checkContext(ctx); err != nil {
		return err
	}
	if err := i.check(authority.EnvironmentReset); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.checkOpen(); err != nil {
		return err
	}
	if i.resetStore == nil {
		return ErrExternalResetUnsupported
	}
	next, err := i.resetStore()
	if err != nil {
		return err
	}
	if err := i.store.Close(); err != nil {
		_ = next.Close()
		return err
	}
	i.store = next
	// Re-derived: the store just closed is the one multipartStore pointed at.
	i.multipartStore, _ = next.(storage.MultipartStore)
	i.usage = Usage{}
	i.multipart = make(map[string]multipartUsage)
	i.multipartTargets = make(map[string]int)
	i.reservedTargets = make(map[string]struct{})
	i.reservedObjects = 0
	i.reservedBytes = 0
	return nil
}

func (i *Instance) Close() error {
	i.closeOnce.Do(func() {
		i.mu.Lock()
		i.closed = true
		store := i.store
		i.mu.Unlock()
		i.closeErr = store.Close()
	})
	return i.closeErr
}
