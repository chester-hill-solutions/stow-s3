# @chester-hill-solutions/stow-s3

TypeScript APIs for native S3 sessions, workspaces and embedded browser storage.

## Install

The TypeScript package is not yet published to npm. v0.3.3 ships native binaries
as [GitHub downloads](https://github.com/chester-hill-solutions/stow-s3/releases/tag/v0.3.3).
Build the TypeScript client from a source checkout:

```sh
git clone https://github.com/chester-hill-solutions/stow-s3
cd stow-s3
make build build-wasm
npm ci --ignore-scripts --omit=optional --prefix packages/stow-s3
npm run build --prefix packages/stow-s3
export STOW_BIN="$PWD/bin/stow-s3"
```

Use `STOW_BIN` for the native process APIs.
Embedded WASM APIs use the built package assets.
Registry publication is deferred. Do not rely on a registry install for v0.3.3.
<!-- repo-only:start -->
See the [release checkpoint](../../docs/release-verification-2026-10-14.md) for availability.
<!-- repo-only:end -->

## Usage

```ts
import { Stow } from "@chester-hill-solutions/stow-s3";

const bucket = await Stow.start({
  dataDir: ".dev-bucket",
  buckets: ["uploads"],
  port: 0,
  backend: "filesystem",
});

process.env.S3_ENDPOINT = bucket.endpoint;
process.env.S3_ACCESS_KEY_ID = bucket.accessKeyId;
process.env.S3_SECRET_ACCESS_KEY = bucket.secretAccessKey;

await bucket.stop();
```

`Stow.start()` owns its child process. Call `stop()` when finished.
Use `backend: "memory"` for temporary storage.
To connect to an existing endpoint, call `Stow.connect({ endpoint, accessKeyId, secretAccessKey, region })`.
Use its `client`, then call `disconnect()` to destroy the SDK client.
Embedded WASM APIs use separate entry points.

Both connection helpers also accept an optional `sessionToken` or credential `provider` for temporary AWS credentials. Run-through starts can set `cacheMaxBytes`, `cacheMaxObjects`, and `cacheTtlSeconds`; the CLI also reads `STOW_CACHE_MAX_BYTES`, `STOW_CACHE_MAX_OBJECTS`, and `STOW_CACHE_TTL`. Live mirror writes require the filesystem backend and a durable coordinated outbox; file-backed workers use expiring claims to coordinate separate processes, while the package never treats an in-memory outbox as durable.

### Embedded host profile

`EmbeddedStow` is a host-neutral typed wrapper for the memory-only WASM bridge. The host loads the `js/wasm` artifact and provides a synchronous `call(request)` method; the wrapper handles object bytes, metadata, capabilities, quotas, reset, and close:

```ts
import { EmbeddedStow } from "@chester-hill-solutions/stow-s3/embedded";

const embedded = EmbeddedStow.open(wasmHost, { maxBytes: 10_000_000 });
embedded.createBucket("assets");
embedded.putObject("assets", "hello.txt", new TextEncoder().encode("hello"));
const object = embedded.getObject("assets", "hello.txt");
embedded.putObject("assets", "hello.txt", new TextEncoder().encode("edited"), {
  ifMatch: object.etag,
});
embedded.close();
```

Conditional writes can use `ifMatch: object.etag` to refuse a stale content save or
`ifNoneMatch: "*"` to create only when absent. A refused condition throws
`EmbeddedStowError` with code `precondition_failed` and preserves the stored object.
Omitted/empty conditions keep unconditional behavior. These are content-ETag checks,
not checks of metadata, complete revision history or the inputs used to produce data.
The bridge advertises `capabilities().conditionalWrites`; conditional requests to an
older host without this capability refuse with `conditional_write_unsupported`.

The Node package includes the tested WASM asset and a ready-made host loader:

```ts
import { EmbeddedStow } from "@chester-hill-solutions/stow-s3/embedded";
import { loadNodeWasmHost } from "@chester-hill-solutions/stow-s3/node-wasm";

const host = await loadNodeWasmHost();
const embedded = EmbeddedStow.open(host, { maxBytes: 10_000_000 });
// use embedded...
embedded.close();
await host.close();
```

Listings are cursor-based as well:

```ts
let page = embedded.listObjects("assets", { limit: 100 });
const objects = [];
for (;;) {
  objects.push(...page.objects);
  if (!page.truncated) break;
  page = embedded.listObjects("assets", { limit: 100, cursor: page.nextCursor });
}
```

Run `make test-wasm` or `make test-node` to rebuild the packaged asset. Custom hosts may still provide their own synchronous `call(request)` implementation.

### Scoped sessions

`withStow` starts a private memory-backed S3 server with a generated bucket and credentials.
It uses a loopback endpoint and cleans up after success, failure or cancellation.

```ts
import { withStow } from "@chester-hill-solutions/stow-s3";

const summary = await withStow(async (session) => {
  await session.s3.send(
    new PutObjectCommand({ Bucket: session.bucket, Key: "input.json", Body: body }),
  );
  const input = await session.s3.send(
    new GetObjectCommand({ Bucket: session.bucket, Key: "input.json" }),
  );
  return summarize(await input.Body.transformToString());
});
```

- Source checkouts use `STOW_BIN`. A platform package can supply the binary in a published distribution.
<!-- repo-only:start -->
  See the [release checkpoint](../../docs/release-verification-2026-10-14.md) before using registry installation.
<!-- repo-only:end -->
- `STOW_*`, `S3_*`, and `AWS_*` are stripped from the child environment, so
  ambient cloud configuration cannot turn a local session into a run-through one.
- Defaults are 16 MiB and 1,000 objects. Override with `maxBytes` and `maxObjects`.
- `dataDir` keeps the session's storage in a directory you name instead of a
  temporary one. A supplied directory selects the filesystem backend, so objects
  persist there and survive `close()`; a session with no `dataDir` is
  memory-backed and disposable.

<!-- repo-only:start -->
See the [session measurements](../../docs/benchmarks/session-baseline.md) for the default budget.
<!-- repo-only:end -->

- `capabilities()` reports what the server is actually enforcing, parsed from the
  versioned readiness message. A limit of `0` means the server reported no limit.
- `handoff()` returns a fresh five-key environment mapping for a child process. It
  never mutates the parent environment and includes nothing else.
- `close()` is terminal and idempotent. A failed callback is rethrown even if
  cleanup also fails, and cleanup removes only a directory the session created.

`openStow` is the same thing without the callback, for manual lifetime control.
`Stow.start()`, `Stow.connect()`, and `EmbeddedStow` are unchanged and remain the
advanced process-owned and external-endpoint APIs.

### Ready-to-work agent workspaces

The `./workspace` entry point wraps the native `stow-s3 workspace` CLI. Prepare
from a task manifest, then launch the agent with the returned
`working_directory` as its cwd:

```ts
import { prepareWorkspace, checkpointWorkspace } from "@chester-hill-solutions/stow-s3/workspace";

const task = await prepareWorkspace("task.json");
// Start your agent process with cwd: String(task.working_directory).
const snapshot = await checkpointWorkspace({ id: String(task.workspace_id) });
```

The wrapper also exports resume, same-machine handoff, diff, restore, portable
checkpoint export, and import helpers. These calls return the CLI's JSON result
and use the same validation and limits as the Go implementation. They require
the package's platform binary or `STOW_BIN` when developing from a checkout.
See [`docs/task-manifest.md`](../../docs/task-manifest.md) for the schema,
archive rules, and the filesystem-isolation boundary. This is a persistent
workspace API; it is separate from the disposable S3 session APIs above.

### Browser persistence profile

The additive [`@chester-hill-solutions/stow-s3/browser`](../../docs/browser-persistence.md) entry
point coordinates durable commits around the embedded memory profile. The
`openBrowserEmbeddedStow` wrapper replays the committed generation during async
open, serializes operations FIFO, and commits each mutation before its promise
resolves. `IndexedDbPersistenceAdapter` uses a manifest, bucket store, and
object store with direct `Uint8Array` values; it also enforces exclusive
namespace ownership:

```ts
import {
  IndexedDbPersistenceAdapter,
  openBrowserEmbeddedStow,
} from "@chester-hill-solutions/stow-s3/browser";

const persistence = new IndexedDbPersistenceAdapter({
  databaseName: "my-app-stow",
});
const embedded = await openBrowserEmbeddedStow(wasmHost, {
  namespace: "app-profile",
  persistence,
  maxBytes: 10_000_000,
});
await embedded.createBucket("assets");
await embedded.putObject("assets", "hello.txt", new TextEncoder().encode("hello"));
await embedded.close();
```

`reset()` clears the persisted generation and live memory state while retaining
ownership. `close()` is terminal, idempotent, drains accepted operations, and
retains data; it does not close the caller-supplied host. Unsupported formats,
stale generations, denied ownership, quota overflow, and closed profiles use
stable persistence error codes rather than falling back to memory-only state.
The existing `EmbeddedStow` and `@chester-hill-solutions/stow-s3/node-wasm` contracts remain
unchanged.

`BrowserFileHistory` layers retained file stages and explicit remote sync over
this profile. Undo/redo and pending sync receipts survive reopen; remote writes
use an expected ETag or expected absence, and conflicts retain local edits.
`S3ObjectSyncRemote` is available from the separate `./browser-s3` entry point.
See [the API and limits](../../docs/browser-file-history.md), or run
`make browser-editor` from the repository root for the offline editor demo.

CLI equivalent:

```sh
stow serve --data-dir .dev-bucket --port 9000
```

## Binary resolution

1. `STOW_BIN` environment variable
2. `bin/stow-s3` relative to the monorepo root (when developing in-repo)
3. `stow` on `PATH`

<!-- repo-only:start -->
## Development

```sh
cd packages/stow-s3
npm install
npm run build
npm test
```

The package test suite includes lifecycle and shared-corpus coverage. Run `make build` before `npm test` when working from the monorepo.
<!-- repo-only:end -->
## Large browser projects and persistent workers

`/project` exports `BrowserProject`, `ContentStore` and `IndexedDbContentAdapter`.
It supports shared chunks, atomic revisions, bounded range reads and conditional manifest sync.
`/worker` exports `AsyncEmbeddedStow`.
`/browser-wasm` provides `loadBrowserPersistentWasmHost` for durable host storage.

Use the [project guide](../../docs/browser-project-storage.md) or [worker guide](../../docs/browser-worker-runtime.md) to get started.

<!-- repo-only:start -->
See the [1 GiB test record](../../docs/benchmarks/browser-project-2026-10-12.json).
<!-- repo-only:end -->
