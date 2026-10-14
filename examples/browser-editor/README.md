# Browser file editor

This demo stores file stages in IndexedDB with the Go WASM runtime.
It supports undo/redo, offline reload and explicit sync to a private native S3 fixture.
The fixture uses generated local credentials and makes no cloud requests.

From the repository root:

```sh
make browser-editor
```

Open http://127.0.0.1:4173. Set `STOW_EDITOR_PORT` to choose another port.
The server starts the fixture, creates its bucket, and seeds `notes.md`.
Stopping the server closes the fixture and removes its temporary data.

Try this workflow:

1. Edit the file and wait for **Saved on this device**.
2. Check **Work offline**, edit again, and use Undo/Redo.
3. Reload. The file, history and selected stage remain in IndexedDB.
4. Uncheck **Work offline** and choose **Sync current stage**.
5. Save another edit, expand **Try a remote conflict**, and simulate another writer.
6. Sync again: the conflict retains your local stages. Choose **Use remote version**,
   then Undo to return to the retained local edit if you want to publish it.

The service worker caches the demo and runtime after **Available offline** appears.
You can then stop the server and reload to exercise actual network loss. Edits
and history still work; sync needs the server running again. The offline checkbox
only disables the remote transport, which is useful for a shorter demonstration.

Each input event starts a full-file save. Wait for confirmation before closing or reloading.
History retains 50 stages within an 8 MiB / 200-object budget.
Server restart resets the fixture but preserves local history. Reconnecting can therefore produce a conflict.

The demo's same-origin proxy uses `S3ObjectSyncRemote` and the AWS SDK to talk to
the real fixture. Production hosts can supply their own proxy or use the S3
transport directly with scoped credentials and appropriate CORS configuration.
See [the library contract and limits](../../docs/browser-file-history.md).

<!-- repo-only:start -->
## Verification

Manually verified in Chrome on 2026-10-12: real WASM and IndexedDB saves;
history/cursor recovery after reload; cached app reload and undo with the server
stopped; explicit fixture sync after reconnecting; competing remote edits refused;
and remote adoption followed by undo and conditional publication of the retained
local edit. Library tests additionally cover lost remote replies, failed local
acknowledgements, quota rollback and protected revisions across reopen.

The workflow is also covered by Playwright with real Chromium, WASM, IndexedDB
and a native fixture. Run `npm run test:browser` in `packages/stow-s3` after building
the artifacts. `npm run test:browser:railway` substitutes the existing Railway
testing bucket and additionally checks direct browser CORS/conditional requests.
It creates a unique disposable prefix, temporarily configures the test origin's
CORS rule, and verifies both object cleanup and policy restoration afterward.
See [the qualification contract](../../docs/browser-file-history.md#repeatable-browser-and-railway-tests).
<!-- repo-only:end -->
