# Browser file history and explicit sync

`BrowserFileHistory` retains file stages, supports undo/redo, and syncs to remote object storage.
It uses `BrowserEmbeddedStow` for WASM storage and durable commits.
A stage is a saved version of one file.
Your application supplies networking and credentials.
The browser API does not run an HTTP S3 server.

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

`read()` returns local bytes without a network request.
On a miss, it fetches the remote object and records its ETag for later writes.
`save()` makes no network request. A new offline file expects the remote key to be absent.
Sync reports a conflict if that key already exists.

`read`, `save`, `undo`, `redo` and `adoptRemote` return `{ data, version, status }`.
`status` and `sync` return `FileHistoryStatus`, which includes:

- retained versions and the selected version ID;
- pending sync state and undo/redo availability;
- the confirmed remote ETag;
- whether an uncertain write needs reconciliation.

Wait for the save promise before marking an edit as saved.
The application owns unsaved input buffers.

`maxVersions` defaults to 50 and must be at least 2.
A save after undo removes the redo branch. Stages beyond the retention limit are released.
Labels do not prevent removal. Undo/redo persists the selected stage and marks it pending
if it differs from the last confirmed remote stage.

## Commit and recovery contract

`BrowserEmbeddedStow.writeBatch()` commits the stage bytes, cursor, retention changes and state record in one transaction.
WASM quota or IndexedDB failures restore the previous committed state.
A save resolves only after the durable commit.
Invalid or missing history records cause an error; Stow does not fetch remote data over local edits.

Before uploading, sync persists the selected revision, remote condition and a random receipt.
It uses `If-Match` for existing files or `If-None-Match: *` for new files.
S3 writes include the receipt in reserved `stow-save-id` metadata.
After remote success, a separate local commit records the acknowledgement.

A lost remote reply or failed local acknowledgement leaves an uncertain attempt.
The next sync compares the remote receipt, bytes, content type and application metadata with that attempt.
If they match, it acknowledges the write without another upload.
Otherwise, it retries the original conditional write.
Unknown failures retain the attempt. A definitive precondition failure reports a conflict.

An attempted revision remains protected if retention removes it from ordinary history.
Sync resolves that attempt before uploading a later stage.
Sync skips intermediate stages that have not been attempted.

A `FileHistoryError` with code `conflict` preserves local stages.
Call `adoptRemote()` to add the current remote bytes as a confirmed stage.
Undo can then select a retained local edit. Its next sync uses the newly observed remote ETag.
Resolve an uncertain attempt with `sync()` before adoption.
Remote deletions cannot currently be adopted; retain or export local data.

Operations run in arrival order across history instances sharing a store and history bucket.
Wait for operations, then close the store and WASM host through their own APIs.
Do not edit the dedicated history bucket outside this API.
Keep the namespace's remote endpoint and account configuration stable.

## Host integration

The core browser entry point has no AWS SDK dependency.
Implement `ObjectSyncRemote` for your proxy, or use `S3ObjectSyncRemote` with a configured `S3Client`.
Your application controls credentials, endpoint, expiration, timeouts and authorization.
The provider must enforce conditional PUT atomically.
A HEAD request followed by an unconditional PUT is insufficient.

For direct S3 access, configure CORS for GET/PUT and all SDK request headers.
Include conditional headers and receipt metadata.
Expose `ETag`, `x-amz-meta-stow-save-id` and the application metadata headers used for recovery.
Use scoped credentials. The demo keeps fixture credentials behind a local same-origin proxy.

The transport reserves `stow-save-id`, regardless of letter case.
A 403 is an access failure, not a missing object.
A 409 retains the uncertain attempt, rather than reporting a definitive conflict.

## Limits of this first slice

- Each stage retains the full file. The bridge buffers and replays the complete namespace in memory.
  Use this profile for small files. For shared assets and streams, use [project storage](browser-project-storage.md).
- History and receipts count toward byte/object quotas. Reserve space for stages, state records and one protected attempt per file.
  `maxVersions` limits stages per file, not the number of files.
- Sync uploads the current file. It does not provide background uploads, deletion sync, bucket mirroring,
  remote change feeds, automatic merge, cross-device history or atomic multi-file undo.
- Browser storage can be evicted. Applications must handle storage errors and provide export or backup as needed.

<!-- repo-only:start -->
Unit tests cover commit, retention, sync recovery and S3 request signing.
Chromium CI tests use real WASM, IndexedDB and Web Locks.
The large project workload and live Railway tests run separately.

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
positive CORS cache age. The runner verifies OPTIONS and PUT responses before starting Chromium. It removes its own rule
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
The manual workflow is available from `main`.
See the [recorded Railway run](assessment-evidence/2026-10-12/railway-browser.json) for local test results.
<!-- repo-only:end -->
