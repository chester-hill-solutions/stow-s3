# Workspace resource paging and host qualification

**Date:** 2026-11-11. **Scope:** W24 development implementation on native macOS
ARM64, source baseline `68132194` plus the working tree.
**Status:** Qualified for the declared local native and OpenCode resource-only profiles.
The [canonical queue](../development/storage-foundation-plan.md) owns status.

## Implementation and review

W24 adds static relative-directory scope, bounded immutable metadata observations,
resource pages and an optional workspace App in the existing browser bundle.
The [contract](../contracts/workspace-resources-contract.md#w24-additive-implementation-contract)
keeps W23 complete-directory resources, current bounded file reads, shared reader
leases and resource-only behavior intact. Usage is in the
[workspace resource guide](../guides/workspace-resources.md).

Independent review found the distinct-root limit was applied before deduplication;
the native scope normalizer now enforces it after deduplication. Paging review
found transient page scratch charged against the observation cache, causing valid
continuations to fail when metadata filled that cache. The adapter separates
those budgets and adds a full-cache continuation regression. Focused independent
recheck accepted the fix, including active evicted observations, error cleanup and
close ordering.

App registration review found no blocker in profile boundaries, escaped bootstrap
JSON or complete encoded UI limits. Rendered verification uncovered replacement
string expansion corrupting the generated JavaScript bundle. Parent integration
review also found an overview parser expecting fields absent from the server DTO,
refresh retrying an expired cursor and empty previews classified from old metadata.
Those corrections passed the final rendered/controller regressions.

Focused F review also required visible workspace identity/read roots and clearing
completed previews on refresh. Both fixes passed recheck. The final rendered
fixture verified keyboard focus, safe text, explicit context-preserving attachments,
vertical scrolling and unclipped navigation at narrow 200% zoom. Parent inspection
confirmed the ordinary view, and the user inspected a separate local sample preview.
Live App-host compatibility remains unverified.

Independent G review found two receipt false positives: combining interim and
terminal assistant messages, and accepting a traversal path merely because it
started with `inputs/`. The runner now groups message/part IDs, validates only the
terminal JSON answer, and independently checks canonical UTF-8 resource paths and
cursor syntax. Focused regressions and independent recheck accepted both fixes.
The guard separately rejects paid model IDs and fallback arrays before upstream
requests; only it holds the real key. Host and Stow environments carry placeholder
and empty credentials respectively.

## Native observation measurements

One sequential fresh process per size on macOS 27.2 ARM64, using Go 1.25.6.
Each fixture contains `inputs/entry-NNNNN.txt` regular files with one byte each.
Only `ObserveDirectory("inputs")` is timed; maximum process RSS includes runtime
and fixture creation. These single local observations are measurements, not
performance thresholds or total process-memory guarantees.

| Entries | Observation time | Charged metadata | Maximum process RSS | Result |
| --- | --- | --- | --- | --- |
| 100 | 7.118 ms | 29,556 bytes | 12,812,288 bytes | Complete |
| 1,000 | 100.692 ms | 293,256 bytes | 15,826,944 bytes | Complete |
| 10,000 | 1,007.128 ms | 2,930,256 bytes | 18,579,456 bytes | Complete |
| 10,001 | 1,117.133 ms | No retained observation | 17,924,096 bytes | Explicit too-large refusal |

The runner uses `OpenWorkspace`, host file writes, `OpenWorkspaceReader` with
`ReadRoots: []string{"inputs"}`, then `ObserveDirectory`. Its output records
`MetadataBytes`, `Len` and `errors.Is(err, ErrWorkspaceReadTooLarge)`; macOS
`/usr/bin/time -l` records maximum RSS. Initial sandbox timing could not obtain
RSS through `sysctl`; the table uses the subsequent reviewed sequential run.

## Verification and host outcome

| Check | Result |
| --- | --- |
| `make build build-wasm` | Passed on the stable Go implementation and first corrected browser artifact. |
| Affected Go race suites | Passed `internal/rooted`, `internal/storage/workspace`, `pkg/stow`, `internal/mcpstorage`, `cmd/stow-s3`. |
| `make test-conformance` | Passed all four memory/filesystem and runtime-backed profiles. |
| API generation and documentation links | Passed; generated references include observations, paging, scope and optional App launch. |
| Independent TypeScript MCP client | Passed against the native stdio binary: 235-entry traversal, stable metadata/current bytes, scope and legacy overflow. |
| Final extension checks | Passed TypeScript, 25/25 tests (including the rendered SDK-bridge Chrome test) and deterministic build verification. |
| `make standards` | Passed in the final clean run. The earlier run stopped at extension lint; scoped repairs and the complete final rerun passed without baseline changes. |
| Coverage | Ratchet passed: 75.56%, 13,848/18,327 statements. |
| Final embedded bundle | Focused workspace/browser MCP and CLI race suites passed again after the final bundle rebuild. |
| Pilot fixture/guard tests | Nine focused guard/verifier/native-fixture regressions passed, including a real loopback listener with mocked upstream; these made no provider calls. |
| `make test-agent` | Final run passed 36 tests; one unrelated legacy real-model opt-in test skipped. The actual resource-host result below is separate. |

Final bundle: 238,711 gzip bytes / 1,009,839 HTML bytes, SHA-256
`472f16fbddf378ed261e0f3c5872991bd47a67ca797dab8a2a5034ab7de290cf`.

## Actual OpenCode resource task

The [sanitized receipt](../assessment-evidence/2026-11-11/workspace-resources-host.json)
records native Stow SHA-256
`f67bd5643fbc5344a27db8d942833152693b79d1da8c7b0b8019358d4238bd19`
and installed OpenCode v2.2.7 SHA-256
`b9d576000191d7f441198076f6d50a843064259709ed31f7de995cbfc540d85f`.
The isolated profile used stdio resources, static `inputs` scope, one user prompt
and a five-minute deadline. First-to-last recorded host events span 48,207 ms;
this excludes setup and is not a total startup-time measurement.

OpenCode completed eight resource-only calls: resource discovery, overview, root
page, input page, continuation, brief and both CSVs. Its terminal JSON identified
`beta`, source A `23`, source Z `24`, and Z-minus-A `1`, with both exact resource
URIs. No native file, shell or subagent tool appears in the completed trace.
All nine recorded provider requests asked for `openrouter/free` and returned 200.
The receipt retains the concrete models selected by Free Router; model suffixes
are not the enforcement boundary. The proxy permitted only the configured free
request route and rejected fallback fields before upstream requests.

Two corrections remain visible in the receipt history. The first setup attempt
refused an unsupported `max_files` manifest field before any provider request;
the fixture now uses `max_objects`, with a native preparation regression. The
actual task initially failed numeric-only verification because the correct CSV
integer values appeared as JSON strings. The task specified field names, not
numeric JSON types. Independent review accepted narrow canonical integer
normalization, with regressions rejecting whitespace, signs, leading zeros,
decimal/exponent strings, suffixes, unsafe values and wrong differences. The same
saved terminal answer and tool trace then passed reprocessing, with no second
model call. Its raw-evidence digest is retained in the receipt.

After completion, no process for that workspace ID and no isolated pilot
directory remained. The credential source and global OpenCode settings were not
modified. The [recipe](../../examples/workspace-resources/README.md) includes the
private profile template; the proxy is acceptance-fixture code, outside Stow's
product runtime.
No SDK fixture or skipped test qualifies the actual model-host gate.

## Remaining scope

Live App-host rendering, Linux runtime, a second physical host, protected
multi-tenant access, checkpoint resources/ranges and workspace managed editing
remain separate gates. No deployment, publication or commit is included.
