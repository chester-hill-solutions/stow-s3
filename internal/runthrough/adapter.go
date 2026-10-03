package runthrough

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// writeAction is the decision for whether a mutating op should touch upstream.
type writeAction int

const (
	writeSkip writeAction = iota
	writeError
	writePropagate
)

// Adapter wraps a local storage.Store with optional upstream read-through caching
// and controlled live writes. It implements storage.Store for s3api routing.
type Adapter struct {
	// authority is the grant this adapter gates on, resolved once at construction
	// from the caller's Authority and the AllowLiveWrites attenuation. Every
	// upstream decision consults this and nothing else, so there is one mechanism
	// rather than two kept in step. See ADR 0010 decision 2.
	authority authority.Authority
	// resourcePolicy narrows authority per object, and policyErr holds a policy
	// that failed validation. Both are read only through allows, so there is one
	// mechanism rather than two kept in step.
	resourcePolicy    *policy.Set
	policyErr         error
	local             storage.Store
	localMultipart    storage.MultipartStore
	cache             storage.Store
	upstream          Client
	outbox            Outbox
	coordinatedOutbox CoordinatedOutbox
	cfg               Config
	separateCache     bool
	durableOutbox     bool
	cacheHits         atomic.Uint64
	cacheMisses       atomic.Uint64
	cacheEvictions    atomic.Uint64
	cacheMu           sync.Mutex
	cacheEntries      map[string]cacheEntry
	// evictionsHeld counts the bulk fills in progress. While it is above zero,
	// trackCacheObject records the object and does not evict; the fill applies the
	// limits once when it releases. Guarded by cacheMu, because it is read on every
	// cache write. See holdEvictions.
	evictionsHeld int
	outboxLocks   outboxKeyLocks
	claimOwner    string
	claimLease    time.Duration
}

// New creates a run-through adapter. upstream may be nil for local-only behavior.
// The local store is also used as the cache for compatibility with existing callers.
func New(cfg Config, local storage.Store, upstream Client) *Adapter {
	return NewWithOutbox(cfg, local, local, upstream, NewMemoryOutbox())
}

// NewWithCache creates an adapter with separate authoritative local and
// upstream-derived cache stores.
func NewWithCache(cfg Config, local, cache storage.Store, upstream Client) *Adapter {
	return NewWithOutbox(cfg, local, cache, upstream, NewMemoryOutbox())
}

// NewWithOutbox creates an adapter with explicit local, cache, upstream, and
// outbox dependencies.
func NewWithOutbox(cfg Config, local, cache storage.Store, upstream Client, outbox Outbox) *Adapter {
	if cache == nil {
		cache = local
	}
	if outbox == nil {
		outbox = NewMemoryOutbox()
	}
	durable := false
	if provider, ok := outbox.(DurableOutbox); ok {
		durable = provider.Durable()
	}
	// Validated once, here, so the answer cannot depend on which reach asked, and
	// held as an error rather than discarded so a widening policy refuses instead of
	// silently answering with the wider environment.
	granted := cfg.effectiveAuthority()
	var narrow *policy.Set
	var policyErr error
	if cfg.ResourcePolicy != nil {
		validated, err := policy.New(*cfg.ResourcePolicy, granted)
		if err != nil {
			policyErr = err
		} else {
			narrow = &validated
		}
	}
	coordinated, _ := outbox.(CoordinatedOutbox)
	localMultipart, _ := local.(storage.MultipartStore)
	return &Adapter{
		cfg:               cfg,
		authority:         granted,
		resourcePolicy:    narrow,
		policyErr:         policyErr,
		local:             local,
		localMultipart:    localMultipart,
		cache:             cache,
		upstream:          upstream,
		outbox:            outbox,
		coordinatedOutbox: coordinated,
		separateCache:     cache != local,
		durableOutbox:     durable,
		cacheEntries:      make(map[string]cacheEntry),
		claimOwner:        newOutboxOwner(),
		claimLease:        defaultOutboxClaimLease,
	}
}

// Config returns the adapter configuration.
func (a *Adapter) Close() error {
	outboxErr := a.outbox.Close()
	localErr := a.local.Close()
	var cacheErr error
	if a.separateCache {
		cacheErr = a.cache.Close()
	}
	return errors.Join(outboxErr, localErr, cacheErr)
}

func (a *Adapter) Config() Config {
	return a.cfg
}

// CacheStats reports process-local cache hit and miss counters for admin
// inspection. They are intentionally not persisted across restarts.
func (a *Adapter) CacheStats() (hits, misses uint64) {
	return a.cacheHits.Load(), a.cacheMisses.Load()
}

// OutboxStats reports pending and terminal entries for admin inspection.
func (a *Adapter) OutboxStats() (pending, terminal int) {
	for _, entry := range a.outbox.Pending() {
		if entry.Terminal {
			terminal++
			continue
		}
		pending++
	}
	return pending, terminal
}

func (a *Adapter) OutboxPreparedStats() int {
	return len(a.preparedEntries())
}

func (a *Adapter) OutboxLastError() string {
	for _, entry := range a.orderingEntries() {
		if entry.LastError != "" {
			return entry.LastError
		}
	}
	return ""
}

func (a *Adapter) OutboxRetryAttempts() uint64 {
	var total uint64
	for _, entry := range a.orderingEntries() {
		total += uint64(entry.Attempts)
	}
	return total
}

// OutboxEntries returns a point-in-time copy for administrative inspection.
func (a *Adapter) OutboxEntries() []OutboxEntry {
	return a.outbox.Pending()
}

// OutboxPreparedEntries returns unresolved write-ahead intents for inspection.
func (a *Adapter) OutboxPreparedEntries() []OutboxEntry {
	return a.preparedEntries()
}

// DiscardOutboxEntry removes one pending or terminal propagation intent.
func (a *Adapter) DiscardOutboxEntry(id string) error {
	for _, entry := range a.outbox.Pending() {
		if entry.ID != id {
			continue
		}
		unlock := a.outboxLocks.lock(outboxIdentity(entry.Bucket, entry.Key))
		defer unlock()
		return a.outbox.Discard(id)
	}
	for _, entry := range a.preparedEntries() {
		if entry.ID != id {
			continue
		}
		if entry.PreparedOwner != "" && (entry.PreparedUntil.After(time.Now().UTC()) || outboxOwnerAlive(entry.PreparedOwner)) {
			return ErrOutboxClaimHeld
		}
		unlock := a.outboxLocks.lock(outboxIdentity(entry.Bucket, entry.Key))
		defer unlock()
		return a.discardPreparedIntent(entry)
	}
	return fmt.Errorf("outbox entry %q not found", id)
}

// upstreamReachable reports whether the upstream is usable for this bucket at all.
// It asks no policy: a policy decides per object and a bucket is not an object, and
// folding the two together put one check on the propagation path twice. See
// "Reaching upstream under a policy" in docs/storage-admission-contract.md.
func (a *Adapter) upstreamReachable(bucket string) bool {
	return !a.cfg.Offline && a.upstreamPermitted(bucket)
}

func (a *Adapter) upstreamPermitted(bucket string) bool {
	if a.upstream == nil {
		return false
	}
	if !a.authority.Allows(authority.UpstreamRead) {
		return false
	}
	return a.cfg.Upstream.Bucket == "" || a.cfg.Upstream.Bucket == bucket
}

// upstreamEnabled combines online reachability and the per-object read policy.
// Cached reads check upstreamPermitted separately so offline can still serve
// authorized cached bytes. Live observation, listing and writes use this gate.
func (a *Adapter) upstreamEnabled(bucket, key string) bool {
	return a.upstreamReachable(bucket) && a.allows(authority.UpstreamRead, bucket, key) == nil
}

// allows is the policy's answer, and the only place the adapter asks one. A policy
// that failed validation refuses every reach.
func (a *Adapter) allows(op authority.Operation, bucket, key string) error {
	if a.policyErr != nil {
		return a.policyErr
	}
	if a.resourcePolicy == nil {
		return nil
	}
	return a.resourcePolicy.Allows(a.authority, policy.Object(bucket, key), op)
}

// decideUpstreamWrite collapses Policy × AllowLiveWrites into one action.
func (a *Adapter) requireDurableOutbox(action writeAction) error {
	if action == writePropagate && (!a.durableOutbox || a.coordinatedOutbox == nil) {
		return ErrDurableOutboxRequired
	}
	return nil
}

func (a *Adapter) decideUpstreamWrite(bucket, key string) writeAction {
	if !a.upstreamEnabled(bucket, key) {
		return writeSkip
	}
	// The grant answers first and on its own. An operation the environment was not
	// given is refused whatever the policy says, because the policy is a
	// configuration choice and the grant is the permission.
	//
	// a.authority was resolved once at construction, folding AllowLiveWrites into
	// the caller's grant. Reading either field here would be the second mechanism
	// ADR 0010 exists to remove - two fields answering the same question, kept in
	// step by hand.
	if !a.authority.Allows(authority.UpstreamWrite) {
		// A policy of mirror-writes is not satisfied by keeping the write local, so
		// refusing to propagate is a failure the caller should see. Read-through
		// with a local cache is satisfied by the local write, so declining to
		// propagate is the policy working, not a failure.
		if a.cfg.Policy == PolicyMirrorWrites {
			return writeError
		}
		return writeSkip
	}
	switch a.cfg.Policy {
	case PolicyMirrorWrites, PolicyReadThroughCache:
		return writePropagate
	default:
		return writeError
	}
}

func (a *Adapter) invalidateCache(ctx context.Context, bucket, key string) {
	if !a.separateCache {
		return
	}
	if err := a.cache.DeleteObject(ctx, bucket, key); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
		return
	}
	a.forgetCacheObject(bucket, key)
}

func (a *Adapter) CreateBucket(ctx context.Context, name string) error {
	return a.local.CreateBucket(ctx, name)
}

func (a *Adapter) DeleteBucket(ctx context.Context, name string) error {
	return a.local.DeleteBucket(ctx, name)
}

func (a *Adapter) HeadBucket(ctx context.Context, name string) (*storage.BucketInfo, error) {
	return a.local.HeadBucket(ctx, name)
}

func (a *Adapter) ListBuckets(ctx context.Context) ([]storage.BucketInfo, error) {
	return a.local.ListBuckets(ctx)
}

func (a *Adapter) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts storage.PutOptions) (*storage.ObjectMeta, error) {
	action := a.decideUpstreamWrite(bucket, key)
	if action == writeError {
		return nil, ErrLiveWritesDisabled
	}
	if err := a.requireDurableOutbox(action); err != nil {
		return nil, err
	}
	var prepared OutboxEntry
	if action == writePropagate {
		unlock := a.outboxLocks.lock(outboxIdentity(bucket, key))
		defer unlock()
		if err := a.rejectPreparedKey(bucket, key); err != nil {
			return nil, err
		}
		previousVersion, err := a.localVersionStrict(ctx, bucket, key)
		if err != nil {
			return nil, err
		}
		prepared, err = a.enqueuePreparedIntentLocked(OutboxPut, bucket, key, previousVersion)
		if err != nil {
			return nil, err
		}
	}

	meta, err := a.local.PutObject(ctx, bucket, key, body, opts)
	if err != nil {
		if prepared.ID != "" {
			return a.reconcilePreparedWrite(ctx, prepared, err)
		}
		return nil, err
	}
	a.invalidateCache(ctx, bucket, key)
	if action == writePropagate {
		if _, err := a.commitPreparedIntent(prepared, objectVersion(meta)); err != nil {
			return meta, storage.CommittedError(err)
		}
		if err := a.completeIntentLocked(ctx, prepared); err != nil {
			return meta, storage.CommittedError(err)
		}
	}
	return meta, nil
}

func (a *Adapter) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *storage.ObjectMeta, error) {
	return a.resolveObject(ctx, bucket, key, true)
}

func (a *Adapter) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	_, meta, err := a.resolveObject(ctx, bucket, key, false)
	return meta, err
}

// resolveObject implements read-through cache with optional revalidation. When
// needBody is false the returned ReadCloser is always nil.
func (a *Adapter) resolveObject(ctx context.Context, bucket, key string, needBody bool) (io.ReadCloser, *storage.ObjectMeta, error) {
	localMeta, localErr := a.local.HeadObject(ctx, bucket, key)
	if localErr == nil {
		// With separate stores, local writes are authoritative and must not be
		// replaced by an upstream revalidation. The legacy single-store mode
		// retains its historical revalidation behavior for compatibility.
		if a.separateCache || a.cfg.Offline || !a.upstreamEnabled(bucket, key) || !a.cfg.Revalidate {
			return a.openLocal(ctx, bucket, key, localMeta, needBody)
		}
		return a.revalidateCachedObject(ctx, bucket, key, localMeta, needBody)
	}
	// The local store is the namespace, not merely a cache: a bucket it was never
	// given does not exist here, and reading through for it would let a client
	// reach any bucket on the upstream by guessing the name.
	if !errors.Is(localErr, storage.ErrObjectNotFound) {
		return nil, nil, localErr
	}
	if !a.upstreamPermitted(bucket) || a.allows(authority.UpstreamRead, bucket, key) != nil {
		return nil, nil, storage.ErrObjectNotFound
	}
	return a.resolveCachedObject(ctx, bucket, key, needBody)
}

func (a *Adapter) refreshFromUpstream(ctx context.Context, bucket, key string, needBody bool) (io.ReadCloser, *storage.ObjectMeta, error) {
	a.cacheMisses.Add(1)
	rc, meta, err := a.upstream.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, err
	}
	cacheStore := a.local
	if a.separateCache {
		cacheStore = a.cache
		if err := cacheStore.CreateBucket(ctx, bucket); err != nil && !errors.Is(err, storage.ErrBucketExists) {
			return nil, nil, err
		}
	}
	// Record which upstream state this copy came from. It is the only provenance
	// available, and a later write needs it to tell "upstream still matches what I
	// based this on" from "somebody else changed it".
	if err := a.saveUpstreamState(bucket, key, UpstreamState{ETag: meta.ETag}); err != nil {
		return nil, nil, err
	}
	cached, err := cacheStore.PutObject(ctx, bucket, key, bytes.NewReader(data), storage.PutOptions{
		ContentType:       meta.ContentType,
		Metadata:          meta.Metadata,
		ChecksumAlgorithm: meta.ChecksumAlgorithm,
		ChecksumValue:     meta.ChecksumValue,
	})
	if err != nil {
		return nil, nil, err
	}
	// cached.Size is the stored length, which is what the cache byte limit is
	// measured against. Passing it in avoids a store round trip to re-read what
	// PutObject just returned.
	if err := a.trackCacheObject(ctx, bucket, key, cached.Size); err != nil {
		return nil, nil, err
	}
	if !needBody {
		return nil, cached, nil
	}
	return io.NopCloser(bytes.NewReader(data)), cached, nil
}

func (a *Adapter) ListObjectsV2(ctx context.Context, bucket string, opts storage.ListOptions) (*storage.ListResult, error) {
	localItems, err := listAllObjects(ctx, a.local, bucket, opts.Prefix)
	if err != nil {
		return nil, err
	}
	if !a.upstreamEnabled(bucket, opts.Prefix) {
		cacheItems, cacheErr := a.listCacheItems(ctx, bucket, opts.Prefix)
		if cacheErr != nil {
			return nil, cacheErr
		}
		return storage.PaginateObjects(mergeObjectLists(localItems, cacheItems), opts), nil
	}
	upItems, upErr := listAllObjects(ctx, a.upstream, bucket, opts.Prefix)
	if upErr == nil {
		return storage.PaginateObjects(mergeObjectLists(localItems, upItems), opts), nil
	}
	cacheItems, cacheErr := a.listCacheItems(ctx, bucket, opts.Prefix)
	if cacheErr != nil {
		return nil, cacheErr
	}
	return storage.PaginateObjects(mergeObjectLists(localItems, cacheItems), opts), nil
}

func (a *Adapter) listCacheItems(ctx context.Context, bucket, prefix string) ([]storage.ObjectMeta, error) {
	if !a.separateCache {
		return nil, nil
	}
	items, err := listAllObjects(ctx, a.cache, bucket, prefix)
	if errors.Is(err, storage.ErrBucketNotFound) {
		return nil, nil
	}
	return items, err
}

// QuotaStore excludes upstream objects and the separately bounded read cache.
func (a *Adapter) QuotaStore() storage.Store { return a.local }
