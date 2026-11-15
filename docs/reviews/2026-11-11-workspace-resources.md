# Native workspace resource qualification

**Date:** 2026-11-11. **Scope:** W23 development implementation on native macOS
ARM64, source baseline `68132194` plus the working-tree extension.
**Contract:** [Workspace file resources v1](../contracts/workspace-resources-contract.md).
The [canonical queue](../development/storage-foundation-plan.md) owns status.

## Implemented behavior

`OpenWorkspaceReader` attaches to an existing registered directory without opening
another writer session or rewriting workspace identity/index data. A shared
advisory lease protects the attachment; registered deletion holds the matching
exclusive lease. Multiple readers coexist with a writer and checkpoint capture.
Close and process death release liveness independently of the writer's lifetime.
Collection reports an attached expired workspace as in use.

The native reader serves regular files and complete bounded direct-child
observations through pinned rooted handles. It excludes private path segments
and sensitive parents, refuses symlinks/special files and detects observable
changes. Bounds are 64 KiB per decoded file, 100 visible children, 10,000 examined
entries, 256 KiB per encoded MCP resource result and eight admitted resource reads.
The read deadline is at most ten seconds, narrowed by configured/request deadlines;
filesystem calls are not preemptible.

`--workspace-files` adds resources to existing workspace checkpoint tools.
`--resources-only` omits tools/prompts and requires the file flag. The same server
surface runs through stdio or streamable HTTP. A single HTTP request/session ending
does not release the server's reader; server shutdown does. Existing object mode
and default workspace mode retain their behavior.

## Review findings and repairs

The independent Luna review covered native lifetime/confinement and MCP routing,
capabilities, errors and bounds. It requested tests for manifest failure after
lease acquisition and direct symlink/FIFO reads, and consistent MCP error mapping.
Those tests and fixes landed; focused recheck found no remaining review blockers.

Parent review also replaced pathname-derived directory metadata with descriptor
reads, excluded every sensitive parent prefix, and reproduced a filesystem Unicode
case alias (`.ſtow` resolves `.stow` on this host). The reader now folds exclusion
comparisons without rewriting requested paths. The exact native alias regression
and complete focused reader race suite pass. Missing, forbidden, malformed and
out-of-scope MCP reads share a path-free resource-not-found result, including SDK
routing failures intercepted by the adapter.

The first integrated test run overlapped the final collection assertion change
and failed against an intermediate test. The stable affected race suite and exact
collection regression passed afterward; the integrated gate was restarted.

## Verification

| Check | Result |
| --- | --- |
| Affected race suites: `internal/rooted`, `internal/storage/workspace`, `pkg/stow`, `internal/mcpstorage`, `cmd/stow-s3` | Passed with reviewed local-listener access; rechecked after privacy/error/collection fixes. |
| Exact Unicode alias test and full `WorkspaceReader` focused race suite | Passed after the alias fix, with the exact test name in verbose output. Final combined reader/resource/CLI race cases passed again on the final implementation. |
| `make test-conformance` | Passed all four memory/filesystem and runtime-backed profiles. |
| Native CLI process cases | Passed normal five-tool compatibility, resource-only capability restrictions, host-written file reads, close/kill release and shared HTTP lifetime. |
| Go quality and file-size ratchets | Passed without baseline changes. |
| API reference generation, documentation links and ADR index | Passed; generated native/MCP references include the development surface. |
| `make standards` | Passed: formatting, vet, full Go races, quality/state/file-size/coverage gates, version/install/site/command/ADR/docs/API/script checks, TypeScript/generated-output and browser checks. Coverage measured 77.3% (13,469/17,425 statements); no baseline was raised. |

The final Unicode comparison repair landed after the integrated Go test phase.
After it, the complete affected reader/MCP/CLI race selection, affected-package
vet, generated API check and quality/file-size gates passed again. Unchanged
storage conformance and browser checks were not needlessly repeated.

Initial sandbox runs could not bind loopback in existing tests. The complete
affected suites and integrated gate use reviewed execution with loopback access.
Temporary Go caches were used. Validation rebuilds package outputs as required;
the pre-existing missing generated distribution files are restored to their
original absent state after validation. No remote provider or real-model test ran.

## Remaining product gates

This record does not qualify Linux runtime, another physical host, protected
multi-tenant authentication, encryption, actual model-host usability, checkpoint
resource ranges or guarded workspace editing. W24–W26 retain those applicable
gates. W27/W28 remain conditional. No release, deployment, publication or commit
was performed. All processes that remove workspaces with attached readers must
use a revision aware of the new reader leases.
