# Run-through mode: the offline cache

Local mode keeps all object data in the selected local backend, and it is the
default. Run-through mode adds an upstream S3 client and a separate cache.

This is the second pillar, and the reason to care is the credential split. The
upstream keys are read by the stow server. An agent pointed at the local endpoint
gets a local key pair that means nothing to the bucket, so it can read and write
the cache and cannot touch the account behind it. Take the network away and the
cached data is still there.

```sh
stow-s3 serve --mode run-through \
  --data-dir /srv/agent-data \
  --cache-dir /srv/agent-cache \
  --upstream-endpoint https://s3.example.com \
  --upstream-bucket datasets
```

The cache is a separate directory from the data directory, and that separation is
load-bearing: the cache is a copy of someone else's data, and a data directory you
can reset should not be a directory that holds it.

A read of an already-cached object falls back to the cached copy when the upstream
cannot be reached, so losing the network does not become the agent's problem. This
is verified rather than asserted: with an upstream killed outright, a read of a
cached key still returns its bytes. A key that was never cached and cannot be
fetched returns an error rather than an empty body — the fallback covers a failed
revalidation, not a missing object.

A note on the shape of the cache: the bucket is created locally first, because
bucket namespace operations stay local. A first read against a bucket that exists
only upstream returns `NoSuchBucket` until the bucket exists locally. That is
deliberate — a run-through server does not create upstream buckets behind your
back — but it surprises people the first time, so it is written down here.

Bound it with `--cache-max-bytes` and `--cache-max-objects` (or
`STOW_CACHE_MAX_BYTES` and `STOW_CACHE_MAX_OBJECTS`), and give entries a lifetime
with `STOW_CACHE_TTL`. The limits are re-applied on startup rather than only on
writes, because the cache directory outlives the process: a server that only ever
reads would otherwise drift past its cap indefinitely.

TTL is measured from the retained cache record's publication time. Reopening
does not restart that deadline; entries without a verifiable publication time
expire when a positive TTL is configured.

## Working with the network gone

`--offline` makes the guarantee checkable rather than merely true. Without it, a
read that cannot be revalidated falls back to the cached copy — which is the right
behaviour, but it is a *consequence of error handling*, so nothing states that the
network was not touched and a hung upstream turns every read into a timeout. With
`--offline` the server makes no upstream call at all: a read is served from the local
store or the cache, or it is a miss.

```sh
stow-s3 serve --mode run-through --offline \
  --data-dir /srv/agent-data --cache-dir /srv/agent-cache
```

`STOW_OFFLINE=true` does the same. The flag wins in both directions, so
`--offline=false` overrides an exported `STOW_OFFLINE=true` for one command — which
matters, because a shell default should not be able to contradict a deliberate
command line on a safety flag.

**Warm the cache first.** `prewarm` fetches a named set of keys so they survive the
network going away:

```sh
stow-s3 prewarm --bucket datasets --data-dir /srv/agent-data \
  --keys models/bert.bin,models/bert.config.json
```

It takes **exact keys, never a prefix**, and a glob is refused with a message that
says why. A prefix on a real bucket is a data-exfiltration shape, and the caller
here is an agent that reads untrusted text: handed "warm everything under `logs/`"
it would have a way to copy a bucket it was never given, and the operator who wrote
the prefix would have no way to tell that from a harmless cache fill. Read the list
from a file with `--keys-file`, one key per line, when the set is long. Blank lines
are skipped, and a line whose first non-space character is `#` is a comment. A `#`
inside a key is a key — `builds/#1234/model.bin` is a real S3 key and is warmed as
one — so a comment has to be on a line of its own. A repeated key is warmed once.

`prewarm --offline` asks the same verb what it can reach without the network, so it
fetches nothing and reports what the cache already holds. It is a flag and not only
`STOW_OFFLINE` because checking a warm-up is what this verb is for, and a caller who
exported the variable in order to warm cannot then pass it to see the result.

Keys that cannot be warmed are reported individually and the rest continue — one
missing key should not abandon a warm-up of a thousand — and the exit status is
nonzero for a partial warm-up, so a script notices.

`cached: true` means the key is in the cache **after the warm finished**, not that it
was fetched. The cache's limits apply while the warm fills it, so warming more keys
than it can hold evicts some of them, and a per-key report taken as each one landed
would claim keys that are not there. If you warmed a thousand keys into a cache that
holds a hundred, the report says a hundred — which is the number you are about to
need when you cut the network. Keys are fetched concurrently and the report keeps the
order you asked in.

`reason` keeps the causes apart, because you can act on each: `not-found` is the
bucket's answer, `offline` is `stow-s3` declining to ask, `not in the cache: the
limits did not leave room for it` means raise the cache, and `held in the local
store, so it was never a cache candidate` means that key was never going to be
cached. A partial warm exits nonzero; a deliberate `--offline` one does not.

The same verb with `--offline` answers the question you actually have after cutting
the network: it fetches nothing and reports what the cache already holds. Nothing it
fails to have is counted as `skipped` rather than `failed`, because an offline
pre-warm is a configured choice and not a broken one — so the exit status is
nonzero for a partial warm and zero for a deliberate one.

**Then ask what you can read.** The inspection route names the cached keys, not just
the counters:

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

`readable: false` means the entry is indexed but its lifetime has passed, so it is
about to be evicted — which is the difference between "you can read this" and "you
can read this until the next sweep". `has_expiry` is beside `expires_at` because an
entry with no lifetime has a zero timestamp, and a zero that means both "never
expires" and "expired long ago" is not a value to branch on. The listing takes
`?bucket=`, `?prefix=` and `?limit=`; the count beside it is the whole cache rather
than the page, so a truncated answer is never mistaken for a complete one. A
malformed `limit` is refused rather than defaulted, because a typo that silently
returns half the list looks exactly like a cache holding half as much.

Counters alone could not answer this. A cache can post a perfect hit rate and hold
nothing the agent asked for, and there was no way to tell those apart from outside.

Run-through is opt-in: pass `--mode run-through` or set `STOW_MODE=run-through`. It is never selected by the presence of credentials, because `AWS_*` variables are exported by CI runners and developer shells for unrelated tools — credentials decide how a requested upstream is authenticated, not whether one is used. `STOW_MODE=local` forces local-only.

In run-through mode, local data is authoritative. Upstream configuration comes from `STOW_*`, `S3_*`, or `AWS_*` environment variables, or from the corresponding command-line options.

`STOW_UPSTREAM_ADDRESSING` selects how stow addresses a bucket on the upstream:
`path` (the default) sends `https://endpoint/bucket/key`, and `virtual-hosted`
sends `https://bucket.endpoint/key`. Some providers serve only one of the two.
The default is path-style because every S3-compatible endpoint accepts it and it
works with bucket names a hostname cannot carry. An unrecognized value is
refused at startup rather than defaulted, because a misspelling that silently
became path-style would send every request somewhere the provider does not
answer.

This names *how* to address a bucket. It is not a request to use an upstream, so
setting it alone never selects one — that is `--mode run-through`, as above. Note
that virtual-hosted addressing needs a resolvable hostname: given a bare IP
address as the endpoint, the AWS SDK keeps the bucket in the path, since a bucket
name cannot be prefixed onto an IP.

Live upstream writes require all of the following:

- run-through mode;
- the filesystem backend;
- explicit live-write opt-in with `--allow-live-writes` or `STOW_ALLOW_LIVE_WRITES=true`;
- a durable coordinated outbox.

`STOW_POLICY=mirrorWrites` is **not** that opt-in. The policy chooses how reads
are routed; the flag is the consent to mutate a real provider, and only the flag
grants it. Setting the policy alone leaves every write local. An explicit
`STOW_ALLOW_LIVE_WRITES=false` is a refusal that the policy cannot override.
See [ADR 0005](adr/0005-live-write-requires-explicit-consent.md).

Stow keeps bucket namespace operations local. It does not create upstream buckets automatically.
