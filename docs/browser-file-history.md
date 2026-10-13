# Browser file history and explicit sync

`BrowserFileHistory`, exported from `@chester-hill-solutions/stow-s3/browser`,
adds retained file stages and remote object sync over `BrowserEmbeddedStow`.
It uses the existing WASM object engine and persistence coordinator. It does
not start an HTTP S3 server in the browser or expose the full native run-through
adapter. Host applications supply networking and credentials.

## Opening and editing

Start with an open browser store and a host-configured AWS SDK `S3Client`:

```ts
import { BrowserFileHistory } from "@chester-hill-solutions/stow-s3/browser";
import { S3ObjectSyncRemote } from "@chester-hill-solutions/stow-s3/browser-s3";

const files = await BrowserFileHistory.open(store, {
  remoteBucket: "my-files",
  remote: new S3ObjectSyncRemote(s3Client),
  maxVersions: 50,
});

const original = await files.read("notes.md");
await files.save("notes.md", new TextEncoder().encode("Edited offline"), {
  label: "Before formatting",
  contentType: "text/markdown",
});
const previous = await files.undo("notes.md");
const next = await files.redo("notes.md");
const status = await files.sync("notes.md");
// status.pending is false only after the selected version is confirmed remotely.
```

`read()` returns cached bytes without a network request. A miss explicitly
fetches the remote object and records its ETag as the base for future writes.
`save()` never contacts the remote. A new offline-created file assumes expected
remote absence; synchronization conflicts if that key already exists remotely.

`read`, `save`, `undo`, `redo`, and `adoptRemote` return `{ data, version, status }`.
`status` and `sync` return `FileHistoryStatus`: retained versions, current version
ID, pending state, undo/redo availability, confirmed remote ETag, and whether a
write requires reconciliation. The app owns its dirty input buffer and must
await the save promise before reporting a local edit as saved.

`maxVersions` defaults to 50 and must be at least 2. A save after undo starts a
new linear branch, releasing the abandoned redo stages. Old stages beyond the
limit are released. Labels name ordinary stages; they do not pin them against
retention. Undo/redo moves the persisted cursor and makes the selected stage
pending when it differs from the last confirmed remote stage.

## Commit and recovery contract

The revision bytes, history cursor, retention changes and state record commit
in one `BrowserEmbeddedStow.writeBatch()` transaction. WASM quota failures and
IndexedDB failures restore the prior committed state. No save resolves on an
in-memory change alone. History records are versioned and validated on load;
missing or malformed local history fails instead of fetching over local edits.

Sync first records a durable attempt with the selected immutable revision,
expected remote identity and a random receipt. The transport enforces `If-Match`
for existing files or `If-None-Match: *` for new files. The remote result is then
acknowledged locally in a separate commit. S3 writes carry the receipt in reserved
`stow-save-id` object metadata.

If the connection or local acknowledgement is lost after remote success, the
next sync compares the remote receipt, bytes, content type and application
metadata with the attempted revision. Matching writes are acknowledged without
uploading again. Otherwise the original conditional write is retried. Unknown
failures retain the attempt; a definitive precondition failure reports a conflict.
A pruned attempted revision stays protected until reconciliation. If a later
local stage exists, sync reconciles the earlier attempt before publishing the
current stage. Unattempted intermediate edits are coalesced.

On `FileHistoryError` with code `conflict`, local stages remain available.
`adoptRemote()` explicitly makes the current remote bytes a new confirmed stage.
Undo can return to a retained local edit; a subsequent sync then conditionally
publishes it against the newly observed remote ETag. An uncertain attempt must be
reconciled with `sync()` before adoption. A deleted remote file cannot currently
be adopted as a deletion; the app should retain/export its local data.

Operations are FIFO across history facades sharing a store and history bucket.
The facade does not own the store or WASM host: await operations, then close those
resources through their existing APIs. Do not edit its dedicated history bucket
outside the facade, or change a namespace's intended remote endpoint/account.

## Host integration

The core browser entry point has no AWS SDK dependency. Implement `ObjectSyncRemote`
for an authenticated host proxy or use `S3ObjectSyncRemote` with a configured
`S3Client`. Credentials, endpoint selection, expiration, request timeouts, and
authorization are caller-owned. Providers must actually enforce atomic conditional
PUT; a HEAD followed by an unconditional PUT cannot fulfill this contract.

For direct browser S3 access, configure CORS for GET/PUT and the SDK's request
headers, including conditional headers and receipt metadata. Expose `ETag` and
`x-amz-meta-stow-save-id` and application metadata response headers needed for
reconciliation. Use scoped credentials;
the demo instead keeps fixture credentials behind a same-origin local proxy.
The transport reserves `stow-save-id` case-insensitively. A 403 is not treated as
a missing object, and a 409 retains the uncertain attempt rather than being
misreported as a definitive conflict.

## Limits of this first slice

- Full bytes are retained per stage. The existing bridge buffers and replays the
  complete local namespace in memory. Use this for modest text files and bounded
  artifacts; streaming, edit deltas and large media need separate work.
- History and receipts count toward the store's byte/object quotas. Configure
  capacity for retained stages, state records and one protected attempted revision
  per file. `maxVersions` bounds each file's history, not the number of files.
- The scope is read-through and explicit current-file PUT. No automatic background
  uploads, file deletion sync, complete bucket mirroring, remote change feed,
  automatic merge, cross-device history, or atomic multi-file undo is provided.
- Browser storage remains subject to the browser's retention policy and user
  clearing. An IndexedDB commit is distinct from a confirmed remote save. Hosts
  can request persistent storage and display/export pending work.
- A network operation currently holds the history queue. Configure bounded
  request timeouts; concurrent editing during long syncs needs further scheduling.
- Multi-tab ownership inherits the persistence adapter's Web Locks requirement.
  Use hosts with Web Locks for cross-context exclusion.

Tests exercise real Go WASM with an injectable transaction store and remote
transport, including failed commits, quota rollback, lost replies, restart
reconciliation, pruning and conflicts. S3 transport tests exercise actual SDK
serialization/signing. The runnable editor supplies manual real-browser coverage;
The browser contract now has a Chromium CI gate and a live Railway qualification
runner. The large project workload is a separate opt-in qualification.

## Repeatable browser and Railway tests

Build the source artifacts and run the local browser suite:

```sh
make build build-wasm
cd packages/stow-s3
npm ci --omit=optional
npx playwright install chromium
npm run test:browser
```

The tests use real Go WASM, IndexedDB, transactions and Web Locks. They exercise
offline app reload, retained undo/redo, transaction aborts, quota rollback,
competing tabs, lost remote responses, failed acknowledgements, and conflicting
writers. The local suite uses a private native S3 fixture; the direct-provider
CORS case runs only in the live profile.

With an authenticated Railway CLI, run `npm run test:browser:railway`. The runner
defaults to the existing `stow-conformance` bucket in `stow-conformance-scratch`.
Other Railway resources can be selected with `--project`, `--environment` and
`--bucket` IDs. Retrieve/build source artifacts before invoking it; CI performs
these build steps explicitly.

The runner keeps bucket credentials in a temporary mode-0600 file and supplies
them to the host process. Browser direct-access tests receive scoped 60-second
presigned requests, with conditional and metadata headers included in the
signature. Each run uses a fresh `stow-browser-tests/<UUID>/` prefix. Only objects
under that prefix are removed; cleanup reads the prefix back to verify it is empty.

Direct tests temporarily add a CORS rule for `http://127.0.0.1:4180`, with explicit
conditional/metadata headers and exposed ETag/receipt fields. Railway requires a
positive CORS cache age. Configuration readback alone did not consistently prove
that object responses had adopted the policy, so the runner verifies actual
OPTIONS and PUT responses before starting Chromium. It removes its own rule
afterward, preserves unrelated rules, and waits for removal to appear on readback.
Credentials are removed even when qualification fails. Live traces are disabled
and signed URLs are redacted from browser diagnostics.

Browser reports are written to `.cache/browser-test-results/`; redacted live
receipts live separately in `.cache/browser-live-receipts/` so a concurrent local
browser run cannot erase them. A filtered diagnostic run is labelled
`partial-pass`, and setup/cleanup failures produce a nonzero exit. A provider
receipt records the testing bucket, run ID, selected tests and cleanup outcomes;
it does not certify the optional large workload or other browsers.

The normal CI workflow runs Chromium against a private fixture. The manual
`browser-live.yml` workflow targets a dedicated `railway-browser` GitHub Actions
environment, using encrypted `STOW_LIVE_ENDPOINT`, `STOW_LIVE_ACCESS_KEY_ID` and
`STOW_LIVE_SECRET_ACCESS_KEY` secrets, plus `STOW_LIVE_BUCKET` and optional
`STOW_LIVE_REGION` variables. The environment is configured with encrypted
Railway testing-bucket credentials and permits deployments only from `main`.
Live testing succeeded locally with Railway CLI credentials; the manual workflow
will be available after these changes are pushed.
