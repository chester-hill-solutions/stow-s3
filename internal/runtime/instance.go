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
	policy           policy.Source
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
	// Initialized for the same reason OpenWithStore initializes, and the asymmetry was a
	// landmine rather than a live defect: normalizeOptions above rejects every backend but
	// memory, so this walk currently finds nothing. It matters the day Open accepts a
	// populated store, because starting usage at zero against existing objects makes
	// MaxBytes and MaxObjects unenforceable, and skips the open-time ErrQuotaExceeded.
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
	// Not resolved here. A policy read once at open could only change by restarting
	// the process, so a revocation was effective when somebody restarted rather than
	// when it was issued.
	multipart, _ := store.(storage.MultipartStore)
	return &Instance{
		store:            store,
		multipartStore:   multipart,
		resetStore:       resetStore,
		options:          options,
		authority:        granted,
		policy:           options.Policy,
		multipart:        make(map[string]multipartUsage),
		multipartTargets: make(map[string]int),
		reservedTargets:  make(map[string]struct{}),
		persistent:       persistent,
	}
}

// checkUpload is checkResource for an operation addressed by upload ID.
//
// The resource comes from this instance's own record rather than the store, because the
// store's answer would have to be read before authorization to know what to authorize. An
// upload this instance cannot resolve therefore has no established resource, which without
// a policy is the old behaviour and with one is refused. Reads i.multipart, so it must be
// called under i.mu, which is why Source must not block.
func (i *Instance) checkUpload(op authority.Operation, uploadID string) error {
	if i.policy == nil {
		return i.authority.Check(op)
	}
	usage, ok := i.multipart[uploadID]
	if !ok {
		return fmt.Errorf("%w: upload %q", ErrResourceUnresolved, uploadID)
	}
	return i.checkResource(op, policy.Object(usage.upload.Bucket, usage.upload.Key))
}

// currentPolicy is the revision in force, validated against the environment it is
// read under — a later revision can be wider than the one before it, so validation
// is per decision too. A nil result means no policy is configured.
func (i *Instance) currentPolicy() (*policy.Set, error) {
	if i.policy == nil {
		return nil, nil
	}
	set, err := i.policy()
	if err != nil {
		return nil, err
	}
	validated, err := policy.New(set, i.authority)
	if err != nil {
		return nil, err
	}
	return &validated, nil
}

// check is the authorization point for operations that name neither a resource nor a
// collection: the environment's own lifecycle, and listing the namespace. It consults the
// policy twice over, and both fix a deny that was written and did not apply. A policy that
// failed validation is refused, because a widening policy means the author believed a
// permission was in force and it is not. And an entry withholding the operation is
// honoured, because Reset names no bucket for a selector to match.
func (i *Instance) check(op authority.Operation) error {
	set, err := i.currentPolicy()
	if err != nil {
		return err
	}
	return i.granted(set, op, "")
}

// checkCollection is check for an operation naming a collection but no resource, so a
// deny is scoped to that collection. See policy.Set.Deny.
func (i *Instance) checkCollection(op authority.Operation, collection string) error {
	set, err := i.currentPolicy()
	if err != nil {
		return err
	}
	return i.granted(set, op, collection)
}

// checkGranted is the answer both of the above give: the environment's, unless a
// policy entry withholds the operation, which is all a policy can say here. granted
// answers for an already-resolved policy, and takes the set because the caller must
// resolve it before the environment is consulted: a widening policy refuses with
// ErrWidening, and checking the environment first would report its reason instead.
func (i *Instance) granted(set *policy.Set, op authority.Operation, collection string) error {
	if err := i.authority.Check(op); err != nil {
		return err
	}
	if set == nil {
		return nil
	}
	return set.Deny(i.authority, policy.LocalNamespace, collection, op)
}

// checkResource is check for an operation on a named resource, and it is where a policy
// is consulted. A nil policy means no policy, so this is check and the answer is
// unchanged. A policy that failed validation refuses here rather than being skipped: the
// author wrote one believing it was in force, and the alternative is answering silently.
func (i *Instance) checkResource(op authority.Operation, res policy.Resource) error {
	if i.policy == nil {
		return i.authority.Check(op)
	}
	set, err := i.currentPolicy()
	if err != nil {
		return err
	}
	return set.Allows(i.authority, res, op)
}

// Authority reports what this environment permits, so an interface can narrow
// its own behaviour to match rather than deciding separately.
func (i *Instance) Authority() authority.Authority {
	return i.authority
}

// Authorize is the decision for one operation on one resource, for a caller owning
// a resource this Instance has no verb for — the workspace facade, whose Destroy was
// consulting the environment only. It is exported rather than left to each caller
// because resolving the revision and deciding must have one implementation.
func (i *Instance) Authorize(op authority.Operation, res policy.Resource) error {
	return i.checkResource(op, res)
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
