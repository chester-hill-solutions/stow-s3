# Bounded native storage dogfood

Run from the repository root on macOS or Linux:

```sh
go run ./examples/storage-dogfood > /tmp/stow-dogfood-evidence.json
```

The executable uses the public Go SDK against disposable filesystem and workspace
stores, with a 30-second context, 1 MiB filesystem byte quota and 16-object limit.
It places synthetic inputs and verification output in Stow and keeps an independent
copy before checking them. All disposable directories are removed on exit; the
stdout report is the retained evidence outside Stow. Failure exits nonzero.
The Linux CI conformance job runs the same harness on every candidate commit.

It checks bytes, ETags, content type and user metadata through real native client
calls, including paged listing, guarded save, stale guard refusal, exact request
retry, changed-input conflict, newer writes followed by replay, close/reopen,
original receipt resolution and a new guard after reopen. It injects an index
publication failure into a separate workspace, checks actual host bytes and
committed-error accounting, repairs the obstruction and verifies metadata after
reopen. The fault and repair operate only on its disposable directory.

This exercises reconnecting a native client to local persisted stores. It does
not exercise a network reconnect, live provider, ChatGPT interface, hostile host
writer or physical power loss. For each future batch, retain the stdout report
and focused regression/CI logs independently. A finite passing run is evidence
for these paths, not a general stability guarantee.
