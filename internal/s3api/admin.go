package s3api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/version"
)

type cacheStatsProvider interface {
	CacheStats() (hits, misses uint64)
}

type cacheEvictionsProvider interface {
	CacheEvictions() uint64
}

// cacheListingProvider is a store that can name the objects it is holding.
//
// The counters an inspection already reports — hits, misses, evictions — say that
// caching is happening and nothing about what is cached. An agent cut off from the
// network needs the second thing, and it cannot be derived from the first: a cache
// can be 100% hit rate and hold nothing the agent asked for.
//
// The type is runthrough's rather than one declared here because this file already
// imports that package, and a second struct describing the same cached objects would
// mean two definitions of one thing to keep in step.
type cacheListingProvider interface {
	CachedObjects() []runthrough.CachedObject
}

type outboxStatsProvider interface {
	OutboxStats() (pending, terminal int)
}

type outboxPreparedStatsProvider interface {
	OutboxPreparedStats() int
}

type outboxHealthProvider interface {
	OutboxLastError() string
	OutboxRetryAttempts() uint64
}

type outboxEntriesProvider interface {
	OutboxEntries() []runthrough.OutboxEntry
}

type outboxPreparedEntriesProvider interface {
	OutboxPreparedEntries() []runthrough.OutboxEntry
}

type outboxAdminProvider interface {
	RetryPending(context.Context) error
	DiscardOutboxEntry(string) error
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/_stow/health":
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	case "/_stow/status":
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		s.writeStatus(w, r)
	case "/_stow/inspect":
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		s.writeInspect(w, r)
	case "/_stow/metrics":
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		s.writeMetrics(w, r)
	case "/_stow/outbox/retry":
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		s.retryOutbox(w, r)
	case "/_stow/outbox/discard":
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		s.discardOutbox(w, r)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}
}

func writeAdminError(w http.ResponseWriter, status int, message string) {
	http.Error(w, message, status)
}

func outboxInspectEntries(entries []runthrough.OutboxEntry) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]interface{}{
			"id":            entry.ID,
			"operation":     entry.Operation,
			"bucket":        entry.Bucket,
			"key":           entry.Key,
			"source_bucket": entry.SourceBucket,
			"source_key":    entry.SourceKey,
			"version":       entry.Version,
			"attempts":      entry.Attempts,
			"terminal":      entry.Terminal,
			"prepared":      entry.Prepared,
			"last_error":    outboxErrorClass(entry.LastError),
			"next_attempt":  entry.NextAttempt,
			"claim_owner":   entry.ClaimOwner,
			"claim_until":   entry.ClaimUntil,
		})
	}
	return out
}

func outboxErrorClass(message string) string {
	if message == "" {
		return ""
	}
	message = strings.ToLower(message)
	switch {
	case strings.Contains(message, "not found"):
		return "not_found"
	case strings.Contains(message, "timeout"), strings.Contains(message, "deadline"):
		return "timeout"
	case strings.Contains(message, "accessdenied"), strings.Contains(message, "signature"), strings.Contains(message, "credential"):
		return "authorization"
	case strings.Contains(message, "quota"), strings.Contains(message, "throttl"):
		return "capacity"
	default:
		return "upstream_error"
	}
}

func (s *Server) writeStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	buckets, err := s.store.ListBuckets(ctx)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "status is temporarily unavailable")
		return
	}
	objectCount := 0
	for _, b := range buckets {
		objects, err := listAllAdminObjects(ctx, s.store, b.Name)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "status is temporarily unavailable")
			return
		}
		objectCount += len(objects)
	}
	addr := s.Addr()
	if addr == "" {
		addr = s.config.Host
	}
	mode := s.config.Mode
	if mode == "" {
		mode = "local"
	}
	cachePolicy := s.config.CachePolicy
	if cachePolicy == "" {
		cachePolicy = "none"
	}
	writePolicy := s.config.WritePolicy
	if writePolicy == "" {
		writePolicy = "local-only"
	}
	payload := map[string]any{
		"mode":         mode,
		"listen":       addr,
		"region":       s.config.Region,
		"bucket_count": len(buckets),
		"object_count": objectCount,
		"cache_policy": cachePolicy,
		"write_policy": writePolicy,
		"uptime_sec":   int(time.Since(s.startTime).Seconds()),
		"version":      version.Version,
	}
	if s.config.UpstreamHost != "" {
		payload["upstream"] = s.config.UpstreamHost
	}
	if provider, ok := s.store.(outboxStatsProvider); ok {
		pending, terminal := provider.OutboxStats()
		payload["outbox_pending"] = pending
		payload["outbox_terminal"] = terminal
	}
	if provider, ok := s.store.(outboxPreparedStatsProvider); ok {
		payload["outbox_prepared"] = provider.OutboxPreparedStats()
	}
	if provider, ok := s.store.(outboxHealthProvider); ok {
		payload["last_upstream_error"] = outboxErrorClass(provider.OutboxLastError())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// defaultCachedKeyLimit bounds a cache listing when the caller does not ask for a
// bound. It is generous enough to answer "what did I warm" for a real session and
// small enough that the response is a document rather than a dump.
const defaultCachedKeyLimit = 500

// maxCachedKeyLimit is the ceiling a caller can ask for. A limit that could be
// unbounded is not a limit, and this route is admin-authenticated but not
// rate-limited, so the bound has to be in the code rather than in the request.
const maxCachedKeyLimit = 10000

// cachedInspectEntry is one cached object as an inspection reports it.
//
// It is declared here rather than reusing runthrough.CachedObject, and the JSON tags
// are here rather than on that type, so the store layer carries no HTTP concern. The
// fields happen to be the same today. They are separate because an inspection is
// allowed to answer a question the store has no opinion about, and it already does:
// a cache entry with no lifetime has a zero expiry, and `omitempty` does not omit a
// zero time.Time, so an agent reading expires_at saw 0001-01-01 and had no way to
// tell "expires at the beginning of time" from "never expires". HasExpiry is the
// same shape WorkspaceSummary uses for has_ttl, for the same reason: a zero that
// means two things is not a value, and the boolean beside it is what a caller
// branches on.
type cachedInspectEntry struct {
	Bucket    string    `json:"bucket"`
	Key       string    `json:"key"`
	Size      int64     `json:"size"`
	Accessed  time.Time `json:"accessed"`
	ExpiresAt time.Time `json:"expires_at"`
	HasExpiry bool      `json:"has_expiry"`
	Readable  bool      `json:"readable"`
}

func cachedInspectEntries(objects []runthrough.CachedObject) []cachedInspectEntry {
	entries := make([]cachedInspectEntry, 0, len(objects))
	for _, object := range objects {
		entries = append(entries, cachedInspectEntry{
			Bucket:    object.Bucket,
			Key:       object.Key,
			Size:      object.Size,
			Accessed:  object.Accessed,
			ExpiresAt: object.ExpiresAt,
			HasExpiry: !object.ExpiresAt.IsZero(),
			Readable:  object.Readable,
		})
	}
	return entries
}

// listCachedObjects returns a page of the cache's contents and the total it holds.
//
// The total is reported alongside the page on purpose. An agent that has been cut
// off needs to know whether the listing is complete, and a page with no total is
// indistinguishable from a cache holding exactly that much — which is the
// difference between "you can read all of it" and "you are missing four keys".
func (s *Server) listCachedObjects(r *http.Request) ([]cachedInspectEntry, int, bool) {
	provider, ok := s.store.(cacheListingProvider)
	if !ok {
		return []cachedInspectEntry{}, 0, true
	}
	all := provider.CachedObjects()

	bucketFilter := r.URL.Query().Get("bucket")
	prefix := r.URL.Query().Get("prefix")
	filtered := make([]runthrough.CachedObject, 0, len(all))
	for _, object := range all {
		if bucketFilter != "" && object.Bucket != bucketFilter {
			continue
		}
		if prefix != "" && !strings.HasPrefix(object.Key, prefix) {
			continue
		}
		filtered = append(filtered, object)
	}

	limit := defaultCachedKeyLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		// A malformed limit is a refusal rather than a default. Defaulting would
		// mean a typo silently returns a truncated answer that looks complete,
		// which for a route whose purpose is "tell me what I can read" is the one
		// wrong thing to do.
		if err != nil || parsed < 0 {
			return nil, 0, false
		}
		if parsed > maxCachedKeyLimit {
			parsed = maxCachedKeyLimit
		}
		limit = parsed
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return cachedInspectEntries(filtered), len(all), true
}

func (s *Server) writeInspect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketFilter := r.URL.Query().Get("bucket")
	buckets, err := s.store.ListBuckets(ctx)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "inspection is temporarily unavailable")
		return
	}

	type bucketSnap struct {
		Name         string `json:"name"`
		ObjectCount  int    `json:"object_count"`
		CreationDate string `json:"creation_date"`
	}
	var snaps []bucketSnap
	multipartUploads := 0
	for _, b := range buckets {
		if bucketFilter != "" && b.Name != bucketFilter {
			continue
		}
		objects, err := listAllAdminObjects(ctx, s.store, b.Name)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "inspection is temporarily unavailable")
			return
		}
		count := len(objects)
		uploads, uploadErr := listAllAdminUploads(ctx, s.multipart, b.Name)
		if uploadErr != nil {
			writeAdminError(w, http.StatusInternalServerError, "inspection is temporarily unavailable")
			return
		}
		multipartUploads += len(uploads)
		snaps = append(snaps, bucketSnap{
			Name:         b.Name,
			ObjectCount:  count,
			CreationDate: formatTime(b.CreationDate),
		})
	}
	var cacheHits, cacheMisses uint64
	if provider, ok := s.store.(cacheStatsProvider); ok {
		cacheHits, cacheMisses = provider.CacheStats()
	}
	var cacheEvictions uint64
	if provider, ok := s.store.(cacheEvictionsProvider); ok {
		cacheEvictions = provider.CacheEvictions()
	}
	// The keys, not just the counters. A `limit` query parameter bounds the
	// listing, because an inspection that walks a large cache to answer a
	// question about one bucket is a denial of service wearing a diagnostic's
	// clothes, and an agent asking "what can I read" wants a page it can hold.
	cachedKeys, cachedTotal, ok := s.listCachedObjects(r)
	if !ok {
		writeAdminError(w, http.StatusBadRequest, "limit must be a non-negative integer")
		return
	}
	outboxPending, outboxTerminal := 0, 0
	if provider, ok := s.store.(outboxStatsProvider); ok {
		outboxPending, outboxTerminal = provider.OutboxStats()
	}
	lastUpstreamError := ""
	var retryAttempts uint64
	if provider, ok := s.store.(outboxHealthProvider); ok {
		lastUpstreamError = outboxErrorClass(provider.OutboxLastError())
		retryAttempts = provider.OutboxRetryAttempts()
	}
	outboxEntries := make([]map[string]interface{}, 0)
	if provider, ok := s.store.(outboxEntriesProvider); ok {
		outboxEntries = outboxInspectEntries(provider.OutboxEntries())
	}
	preparedEntries := make([]map[string]interface{}, 0)
	if provider, ok := s.store.(outboxPreparedEntriesProvider); ok {
		preparedEntries = outboxInspectEntries(provider.OutboxPreparedEntries())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"buckets":               snaps,
		"multipart_uploads":     multipartUploads,
		"cache_hits":            cacheHits,
		"cache_misses":          cacheMisses,
		"cache_evictions":       cacheEvictions,
		"cached_keys":           cachedKeys,
		"cached_key_count":      cachedTotal,
		"outbox_pending":        outboxPending,
		"outbox_terminal":       outboxTerminal,
		"outbox_entries":        outboxEntries,
		"outbox_prepared":       preparedEntries,
		"last_upstream_error":   lastUpstreamError,
		"outbox_retry_attempts": retryAttempts,
	})
}

func (s *Server) retryOutbox(w http.ResponseWriter, r *http.Request) {
	provider, ok := s.store.(outboxAdminProvider)
	if !ok {
		http.Error(w, "outbox administration is unavailable", http.StatusNotImplemented)
		return
	}
	if err := provider.RetryPending(r.Context()); err != nil {
		writeAdminError(w, http.StatusInternalServerError, "outbox retry failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) discardOutbox(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	provider, ok := s.store.(outboxAdminProvider)
	if !ok {
		http.Error(w, "outbox administration is unavailable", http.StatusNotImplemented)
		return
	}
	if err := provider.DiscardOutboxEntry(id); err != nil {
		if errors.Is(err, runthrough.ErrOutboxClaimHeld) {
			writeAdminError(w, http.StatusConflict, "outbox entry is currently claimed")
			return
		}
		writeAdminError(w, http.StatusNotFound, "outbox entry was not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) writeMetrics(w http.ResponseWriter, r *http.Request) {
	var hits, misses uint64
	if provider, ok := s.store.(cacheStatsProvider); ok {
		hits, misses = provider.CacheStats()
	}
	var evictions uint64
	if provider, ok := s.store.(cacheEvictionsProvider); ok {
		evictions = provider.CacheEvictions()
	}
	pending, terminal := 0, 0
	if provider, ok := s.store.(outboxStatsProvider); ok {
		pending, terminal = provider.OutboxStats()
	}
	prepared := 0
	if provider, ok := s.store.(outboxPreparedStatsProvider); ok {
		prepared = provider.OutboxPreparedStats()
	}
	var retryAttempts uint64
	if provider, ok := s.store.(outboxHealthProvider); ok {
		retryAttempts = provider.OutboxRetryAttempts()
	}
	multipartUploads := 0
	buckets, err := s.store.ListBuckets(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "metrics are temporarily unavailable")
		return
	}
	for _, bucket := range buckets {
		uploads, err := listAllAdminUploads(r.Context(), s.multipart, bucket.Name)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "metrics are temporarily unavailable")
			return
		}
		multipartUploads += len(uploads)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(w, "# HELP stow_cache_hits_total Cache hits observed by the runtime.\n# TYPE stow_cache_hits_total counter\nstow_cache_hits_total %d\n", hits)
	_, _ = fmt.Fprintf(w, "# HELP stow_cache_misses_total Cache misses observed by the runtime.\n# TYPE stow_cache_misses_total counter\nstow_cache_misses_total %d\n", misses)
	_, _ = fmt.Fprintf(w, "# HELP stow_cache_evictions_total Cache entries evicted by policy.\n# TYPE stow_cache_evictions_total counter\nstow_cache_evictions_total %d\n", evictions)
	_, _ = fmt.Fprintf(w, "# HELP stow_multipart_uploads Active multipart uploads.\n# TYPE stow_multipart_uploads gauge\nstow_multipart_uploads %d\n", multipartUploads)
	_, _ = fmt.Fprintf(w, "# HELP stow_outbox_pending_entries Pending outbox entries.\n# TYPE stow_outbox_pending_entries gauge\nstow_outbox_pending_entries %d\n", pending)
	_, _ = fmt.Fprintf(w, "# HELP stow_outbox_terminal_entries Terminal outbox entries.\n# TYPE stow_outbox_terminal_entries gauge\nstow_outbox_terminal_entries %d\n", terminal)
	_, _ = fmt.Fprintf(w, "# HELP stow_outbox_prepared_entries Prepared outbox entries awaiting reconciliation.\n# TYPE stow_outbox_prepared_entries gauge\nstow_outbox_prepared_entries %d\n", prepared)
	_, _ = fmt.Fprintf(w, "# HELP stow_outbox_retry_attempts_total Total upstream propagation attempts.\n# TYPE stow_outbox_retry_attempts_total counter\nstow_outbox_retry_attempts_total %d\n", retryAttempts)
}
