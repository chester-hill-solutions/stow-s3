package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// prewarm fetches a named set of keys into the run-through cache, so they are
// readable with the network gone.
//
// It is a verb rather than a flag on `serve` because the setup it performs is a
// one-off an operator or an orchestrator runs before cutting an agent off, and
// because a long-lived server that pre-warmed on demand would be a server quietly
// reaching for the network at a moment nobody chose.
//
// The key list is explicit and never a prefix. That is the decision this verb
// exists to get right: a prefix on a real bucket is a data-exfiltration shape, and
// the caller here is an agent that reads untrusted text. "Warm everything under
// logs/" is a way to copy a bucket the agent was never given, and the operator who
// wrote the prefix would have no way to tell that from a harmless cache fill.
type prewarmResult struct {
	Version int            `json:"version"`
	Bucket  string         `json:"bucket"`
	Results []prewarmEntry `json:"results"`
	// Warmed and Failed are the two numbers a caller checks, and they are here as
	// well as derivable from the results because "did my warm-up work" should not
	// require counting an array.
	Warmed int `json:"warmed"`
	Failed int `json:"failed"`
	// Skipped is how many keys were named but not attempted, which happens only when
	// the adapter is offline. Reported separately from Failed because an offline
	// pre-warm is a configured choice, not a failure, and lumping the two together
	// would make a deliberate configuration look like a broken one.
	Skipped int `json:"skipped"`
}

type prewarmEntry struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	Cached bool   `json:"cached"`
	Bytes  int64  `json:"bytes"`
	Reason string `json:"reason,omitempty"`
}

func prewarm(args []string) error {
	flags := flag.NewFlagSet("prewarm", flag.ContinueOnError)
	bucket := flags.String("bucket", "", "Upstream bucket to warm (required)")
	keysFlag := flags.String("keys", "", "Comma-separated keys to warm (required; there is no prefix form)")
	keysFile := flags.String("keys-file", "", "Read the key list from this file, one key per line")
	dataDir := flags.String("data-dir", "", "Local data directory")
	cacheDir := flags.String("cache-dir", "", "Cache directory (default: <data-dir>/cache)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	keys, err := readPrewarmKeys(*keysFlag, *keysFile)
	if err != nil {
		return err
	}
	if *bucket == "" {
		return errors.New("prewarm requires --bucket")
	}
	if len(keys) == 0 {
		return errors.New("prewarm requires at least one key: pass --keys or --keys-file")
	}
	entries, err := runPrewarm(*dataDir, *cacheDir, *bucket, keys)
	if err != nil {
		return err
	}
	return reportPrewarm(*bucket, keys, entries)
}

// runPrewarm builds the adapter and warms the keys, returning one entry per key.
//
// Split out of prewarm so the verb reads as the three things it does: read the
// arguments, warm the cache, report what happened. A 99-line verb with the wiring
// inline is a function nobody can review, and the wiring is the part that has to be
// right — it is where the local namespace is created, and without that every key
// reports "bucket not found" instead of being fetched.
func runPrewarm(dataDir, cacheDir, bucket string, keys []string) ([]runthrough.PrewarmResult, error) {
	cfg, err := runthrough.ConfigFromEnvChecked()
	if err != nil {
		return nil, err
	}
	if dataDir != "" {
		cfg.CacheDir = cacheDir
		if cacheDir == "" {
			cfg.CacheDir = dataDir + "/cache"
		}
	} else if cacheDir != "" {
		cfg.CacheDir = cacheDir
	}
	// The upstream client is built before any store is opened, so a run with no
	// upstream configured fails without creating a cache directory. DetectMode
	// would consult ambient credentials, which is exactly what this verb must not
	// do: it runs on the way to cutting an agent off, and a stray AWS profile in
	// the environment would turn a warm-up into reads against a real account.
	upstream, err := runthrough.NewS3Client(cfg.Upstream)
	if err != nil {
		return nil, fmt.Errorf("prewarm needs an upstream: %w", err)
	}

	local := openLocalStore(runtime.BackendFilesystem, dataDir)
	defer local.Close()
	cache := openLocalStore(runtime.BackendFilesystem, cfg.CacheDir)
	defer cache.Close()
	// The local and cache namespaces have to exist before a read can be a miss.
	// Both storage implementations report a missing bucket as ErrBucketNotFound
	// rather than ErrObjectNotFound, and the read path treats a missing parent as a
	// hard error instead of a miss — so a pre-warm against a fresh data directory
	// would report "bucket not found" for every key rather than fetching them.
	// Creating the namespace is what makes the first read a miss about the object
	// rather than about its container.
	for _, store := range []storage.Store{local, cache} {
		if err := store.CreateBucket(context.Background(), bucket); err != nil && !errors.Is(err, storage.ErrBucketExists) {
			return nil, fmt.Errorf("create the %q namespace: %w", bucket, err)
		}
	}

	adapter := runthrough.NewWithCache(cfg, local, cache, upstream)
	// The index is what the cache listing reads and what eviction plans from, and it
	// does not survive a process. Reconciling first means the keys this run warms
	// join an index that already knows about the ones already there, so the limits
	// are enforced against the whole cache rather than against this run's additions.
	if err := adapter.ReconcileCacheIndex(context.Background()); err != nil {
		return nil, fmt.Errorf("reconcile the cache index: %w", err)
	}

	return adapter.Prewarm(context.Background(), bucket, keys), nil
}

// reportPrewarm prints the result and decides the exit status.
func reportPrewarm(bucket string, keys []string, entries []runthrough.PrewarmResult) error {
	out := prewarmResult{Version: 1, Bucket: bucket, Results: make([]prewarmEntry, 0, len(entries))}
	for _, entry := range entries {
		out.Results = append(out.Results, prewarmEntry{
			Bucket: entry.Bucket, Key: entry.Key, Cached: entry.Cached,
			Bytes: entry.Bytes, Reason: entry.Reason,
		})
		switch {
		case entry.Cached:
			out.Warmed++
		case entry.Reason == "offline":
			out.Skipped++
		default:
			out.Failed++
		}
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	// A partial warm-up is a failure for a script's purposes. The detail is on
	// stdout and in the exit status, and a caller that wants to proceed anyway can
	// read which keys are missing.
	if out.Failed > 0 {
		return fmt.Errorf("prewarm could not warm %d of %d keys", out.Failed, len(keys))
	}
	return nil
}

// readPrewarmKeys reads the key list from the flag or the file, and refuses
// anything that is not a plain key.
//
// Duplicates are dropped rather than refused: the same key named twice by an
// operator assembling a list is a mistake worth tolerating, and warming it twice
// costs one fetch.
func readPrewarmKeys(flagValue, file string) ([]string, error) {
	raw := flagValue
	if file != "" {
		if raw != "" {
			return nil, errors.New("pass --keys or --keys-file, not both")
		}
		contents, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read the key list: %w", err)
		}
		raw = string(contents)
	}
	seen := map[string]bool{}
	keys := make([]string, 0, 16)
	for _, line := range strings.Split(raw, ",") {
		for _, part := range strings.Split(line, "\n") {
			key := strings.TrimSpace(part)
			if key == "" || strings.HasPrefix(key, "#") {
				continue
			}
			if strings.ContainsAny(key, "*?[]") {
				// A glob is a prefix wearing a different hat, and the whole point of
				// this verb is that there is no prefix form. Refusing it with a
				// message that says why is better than expanding it and warming more
				// than anyone asked for.
				return nil, fmt.Errorf("key %q looks like a pattern; prewarm takes exact keys, not globs", key)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys, nil
}
