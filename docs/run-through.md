# Run-through mode: the offline cache

Run-through mode keeps a local cache of an S3-compatible upstream.
Local mode is the default. To use an upstream, pass `--mode run-through` or set `STOW_MODE=run-through`.
Credentials alone do not select an upstream.

The server holds upstream credentials. Applications use local credentials to access Stow.
Cached data remains readable when the upstream is unavailable, while the cache retains it.

```sh
STOW_ENDPOINT=https://s3.example.com STOW_BUCKET=datasets \
stow-s3 serve --mode run-through \
  --data-dir /srv/agent-data \
  --cache-dir /srv/agent-cache
```

Keep the cache and data directories separate. The cache contains upstream copies;
the data directory contains authoritative local objects.

Create each bucket locally before reading upstream objects through it.
A bucket that exists only upstream returns `NoSuchBucket`.
Stow does not create upstream buckets.

An online read can fetch and cache an object. If revalidation fails, Stow can return a retained cached copy.
An uncached key that cannot be fetched returns an error.

## Cache limits and expiration

Set byte and object limits with `--cache-max-bytes` and `--cache-max-objects`, or
`STOW_CACHE_MAX_BYTES` and `STOW_CACHE_MAX_OBJECTS`.
Set entry lifetime with `--cache-ttl` or `STOW_CACHE_TTL`. Stow reapplies limits on startup.

An object larger than the entire byte budget bypasses cache storage. GET streams
the observed upstream version; HEAD and prewarm return metadata without consuming
its body. Any older cached copy of that key is removed, while unrelated keys
remain cached. The oversized object is unavailable offline.

TTL starts at the cache record's publication time. Reopening does not extend it.
With a positive TTL, an entry without a verifiable publication time expires.

## Working with the network gone

Pass `--offline` to prevent all upstream requests. Reads use local objects or retained cached copies.
A missing object remains a miss.

```sh
stow-s3 serve --mode run-through --offline \
  --data-dir /srv/agent-data --cache-dir /srv/agent-cache
```

`STOW_OFFLINE=true` has the same effect. The command-line flag takes precedence:
`--offline=false` overrides that environment setting.

### Prewarm exact keys

Fetch the keys needed for offline work:

```sh
stow-s3 prewarm --bucket datasets --data-dir /srv/agent-data \
  --keys models/bert.bin,models/bert.config.json
```

`prewarm` accepts exact keys. It rejects prefixes and globs.
For a longer list, use `--keys-file` with one key per line.
Blank lines and lines beginning with optional whitespace followed by `#` are ignored.
A `#` inside a key remains part of that key. Duplicate keys are fetched once.

Keys are fetched concurrently; results preserve input order.
A failure for one key does not stop the others. A partial online warm exits nonzero.
`prewarm --offline` fetches nothing, reports current cache contents, and exits zero for uncached keys.

`cached: true` means the key remains cached after the whole warm completes.
Cache limits can evict keys fetched earlier in the same warm.

Use `reason` to interpret a result:

| Reason | Meaning |
| --- | --- |
| `not-found` | The upstream did not contain the key. |
| `offline` | Stow made no upstream request. |
| `not in the cache: the limits did not leave room for it` | Cache limits prevented retention. |
| `held in the local store, so it was never a cache candidate` | The authoritative local object takes precedence. |

### Inspect cached keys

```sh
curl -s -H "X-Stow-Admin: $TOKEN" http://127.0.0.1:PORT/_stow/inspect
```

```json
{
  "cache_hits": 0,
  "cached_key_count": 3,
  "cached_keys": [
    {
      "bucket": "datasets",
      "key": "models/bert.bin",
      "size": 411000000,
      "accessed": "2026-09-28T09:14:02Z",
      "expires_at": "2026-10-28T09:14:02Z",
      "has_expiry": true,
      "readable": true
    }
  ]
}
```

`readable: false` marks an expired entry awaiting eviction.
Check `has_expiry` before interpreting `expires_at`; a zero timestamp can mean no expiry.

The listing accepts `?bucket=`, `?prefix=` and `?limit=`. Invalid limits are rejected.
`cached_key_count` counts the whole cache, including entries outside the returned page.

## Serving a shared cache node

A cache node serves several machines instead of one: a team, a build fleet, or a group of agents that share one warm copy of upstream data.
Each client uses the node with the same S3 client it already uses, and the upstream credentials stay on the node.

The safest shape keeps the process on loopback and terminates TLS in a proxy on the same host, so the store is never exposed on another interface:

```sh
# The proxy holds the certificate and forwards to this process on loopback.
STOW_ENDPOINT=https://s3.example.com STOW_BUCKET=datasets \
STOW_ADMIN_TOKEN="$ADMIN_TOKEN" \
stow-s3 serve --mode run-through \
  --data-dir /srv/cache-node/data \
  --cache-dir /srv/cache-node/cache \
  --access-key "$ACCESS_KEY" --secret-key "$SECRET_KEY"
```

A process that must listen on another interface needs the consent flag and provisioned credentials that [the server guide](running-and-probing.md#serving-beyond-loopback) describes.
That shape is for a network you control, and the proxy shape above is still the one to prefer.

Prepare the node the way a single machine is prepared:

1. Create each served bucket locally first. A bucket that exists only upstream returns `NoSuchBucket`, and Stow does not create upstream buckets.
2. Warm the keys the fleet reads first, with `prewarm`, so the first cold window is paid once instead of per client.
3. Raise `--max-concurrent-requests` above the default 16 when many clients share the node.

Watch a running node through its diagnostics, which need the admin token from another address:

- `/_stow/status` for mode, policy, version and uptime.
- `/_stow/inspect` for cache hits, misses and evictions, the cached keys themselves, and the outbox entries with their last error class.
- `/_stow/metrics` for the same counters in Prometheus form.

A read that the node cannot fetch returns an error; a read it can serve from cache keeps working while the upstream is unreachable.
A live write is accepted locally and propagated by the outbox, so `outbox_terminal` and `last_upstream_error` are the two numbers that say whether the node is still keeping its half of that promise.

Operational limits to plan around:

- One process owns one data directory. The directory carries an exclusive lock, so a node scales by serving more clients, not by running several processes over the same directory.
- Upstream credentials live on the node, which is why the node is the only thing that reaches upstream. Per-client scoped credentials are not implemented yet ([ADR 0016](adr/0016-scoped-access-encrypted-cache.md)); every client of a node shares one identity.
- The MCP surface is stdio-only, so an agent on another host reaches the node over S3 rather than through MCP.

## Upstream configuration

Upstream settings come from command-line options or `STOW_*`, `S3_*` and `AWS_*` environment variables.
Set `STOW_BUCKET` to restrict upstream reads to one bucket. Prewarming does not create a key allowlist.
`STOW_MODE=local` selects local-only storage.

`STOW_UPSTREAM_ADDRESSING` accepts:

- `path` (default): `https://endpoint/bucket/key`;
- `virtual-hosted`: `https://bucket.endpoint/key`.

Choose the style your provider supports. Unknown values fail at startup.
Setting an addressing style does not enable run-through mode.
Virtual-hosted addressing needs a hostname; for an IP endpoint, the AWS SDK keeps the bucket in the path.

## Live writes

Local data is authoritative. Upstream propagation requires all of these:

- run-through mode;
- the filesystem backend;
- `--allow-live-writes` or `STOW_ALLOW_LIVE_WRITES=true`;
- a durable coordinated outbox.

`STOW_POLICY=mirrorWrites` selects routing policy; it does not authorize upstream writes.
Without live-write consent, writes stay local.
An explicit `STOW_ALLOW_LIVE_WRITES=false` also prevents propagation.
See the [outbox guide](outbox.md) for retries and conflict recovery.

<!-- repo-only:start -->
See [ADR 0005](adr/0005-live-write-requires-explicit-consent.md).
<!-- repo-only:end -->
