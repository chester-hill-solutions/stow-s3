# Aboiteau native workspace consumer

Status: native B2 startup integration and C2 R2 recovery are recorded as passed in
the consumer plan. Two local cached-image smoke runs under QEMU failed/inconclusive.
The first stalled during final observation; after an authorized Docker Desktop
restart, one retry failed before the lifecycle stream with an OOM event recorded
on the main container. All owned containers were later confirmed absent. See the
[first recheck](../reviews/2026-11-12-aboiteau-consumer-recheck.md) and
[retry review](../reviews/2026-11-12-aboiteau-consumer-retry.md).
Scheduling/status remain in the [canonical plan](../development/storage-foundation-plan.md#aboiteau-native-consumer-intake--2026-11-11).
This note expands existing W items and creates no independent implementation queue.

## Consumer and historical gap

Aboiteau (`cf-opencode`, sibling local checkout) runs OpenCode in a Linux Cloudflare
Container and retains R2 workspace and separate OpenCode-state recovery archives.
At the 2026-11-11 intake, `control-server.ts:startStow` still launched generic
`serve --backend filesystem` with `--data-dir /workspace/.stow`. That opened
object records, not a registered Workspace over ordinary project files; the
workspace tar excluded `.stow`, leaving object-only data outside that archive.
This was a consumer integration gap, not a defect in Stow's same-byte Workspace
contract.

The current `cf-opencode` source has since integrated native Workspace prepare,
identity retention and `workspace serve` readiness in B2. Its controller hash
`e70c25065a74c2d5384df19dd8c1612949b1b08d790b368a72cd0f5212cdd3a3` matches the
cached image provenance, and the parent’s read-only probe matched all 12 Docker
COPY inputs. The consumer plan records B2 and the separate C2 R2 recovery check
as passed. The first local cached-image full-smoke recheck reached 21 checks
before its lifecycle helper failed to observe final state; Docker stopped
responding during cleanup. After an authorized Docker Desktop restart, the one
retry reached 21 checks but failed before the lifecycle stream, with an OOM event
recorded on the main container. Neither run establishes a lifecycle bug. Docker
later confirmed old and retry-owned test containers absent. Native-amd64 CI and
C2 remain prior evidence, not results from these QEMU runs.

Reuse [native Workspace usage](../guides/portable-workspace-usage.md),
[workspace serve](../../cmd/stow-s3/workspace_serve.go) and the
[versioned launcher/readiness path](../../packages/stow-s3/src/workspace-serve.ts).
Do not change the default generic `serve` profile, infer a workspace from cwd,
open an independent store over the same data, or add an execution framework.
Aboiteau's `docs/plans/stow-workspace-integration.md` owns its R0-15 repair and
later artifact-view adoption; the selected consumer code and deployment identifier
remain `cf-opencode` until its own rename/cutover.

## Ownership and work mapping

| Existing work | Stow responsibility | Aboiteau responsibility |
| --- | --- | --- |
| W19 caller acceptance | Document/test supported native prepare/resume/serve/readiness behavior and concrete artifact/platform limits. Reuse existing fixtures. | Repair startup, retain workspace/registry identity, consume reported endpoint/region/bucket/credentials and clean up on partial startup. |
| W20 artifact qualification | Pin and qualify the exact native artifact used by the consumer, including real Linux execution. Distinguish released 0.3.7 from development additions. | Select that artifact, keep its provenance with the image and test startup/restore in the target container. No implicit use of unreleased sources. |
| W21 named consumer recipe | Add Aboiteau as a native Workspace consumer alongside the existing object/transfer consumers; retain a reproducible synthetic recipe and any demonstrated reusable setup need. | Verify disk ↔ S3 and R2-only cold recovery; preserve existing generic-store data or block cutover until migration is verified. |
| W23/W24 live resources | Preserve read-only attachments, explicit scope, bounded pages, expiry and removal coordination; provide the supported native/MCP access recipe. | Add authorized artifact browsing without a shell/model round trip; enforce tenant/user access outside repository execution. |
| W25 saved resources | Preserve checkpoint identity/digest, verified bounded reads and reader-versus-prune guarantees. | Distinguish live from saved views; map displayed review points to actual saved bytes. Keep existing R2 recovery until equivalence is separately demonstrated. |
| W26 managed editing | Complete public save/resolve, durable uncertainty/recovery and applicable host qualification before adoption. Internal publication alone is insufficient. | Defer human edits until then; quiesce OpenCode, background tools and S3 writers; honor stale/unknown results without blind retry. |

This is a named candidate, not a completed meaningful-model acceptance, independent
team or repeat-use event. W22 evidence needs actual use and measurements. Do not
mark W19/W20/W21 or W23–W26 complete from this plan update.

## First consumer acceptance: same files and recovery

### Local consumer recheck boundary — 2026-11-12

The user authorized the real consumer test. The current sibling checkout already
contains native Workspace startup and recovery integration; the original intake
gap was not reimplemented. The full `scripts/smoke-container.sh` ran from private
copies against the cached immutable image after all 12 Docker COPY inputs matched.
Both QEMU runs failed before full completion: the first at final state observation,
the retry before the lifecycle stream after an OOM event on the main container.
The [first review](../reviews/2026-11-12-aboiteau-consumer-recheck.md) and
[retry review](../reviews/2026-11-12-aboiteau-consumer-retry.md) distinguish these
outcomes. After the authorized Docker Desktop restart, cleanup of both runs was
confirmed. No model, Cloudflare/R2 operation, consumer source edit, publication or
deployment occurred. Native-amd64 CI and C2 remain separate prior evidence.

Use disposable synthetic workspaces and no provider/model calls for the storage
contract. Record artifact/source hashes, OS/architecture and exact commands.

1. Prepare/open an ordinary project directory as a registered Workspace, retain its
   registry/identity, and serve the same owner through the supported facade. Verify
   readiness reports the correct root/identity/capabilities and endpoints; health
   alone does not prove backend selection. Partial failure releases ownership and
   children. Stop/resume must not create a second writer.
2. Write a file directly and read identical bytes through S3; write/overwrite/delete
   through S3 and verify the expected ordinary file result. Include binary data and
   range reads. Test the supported key-to-path rules rather than treating every S3
   key as a natural path.
3. Inventory ordinary files, escaped/reserved keys, object metadata, unfinished
   multipart state and registry metadata. Define preserved/rebuilt/unsupported
   behavior for each. Do not describe `.stow` as wholly disposable: it can contain
   authoritative metadata or logical objects. Clarify exclusions in the consumer
   recipe and refuse unsupported writes or preserve their data separately.
4. Reconstruct from the consumer's verified external archives with the original
   container disk and registry unavailable; prove the same intended working bytes
   and S3 behavior after restart. Stow owns its supported reconstruction contract;
   Aboiteau owns R2 upload/encryption/publication, Git and OpenCode SQLite/session
   preservation. Stow file checkpoints exclude different data and cannot silently
   replace the consumer's recovery pair. Full-disk snapshots cannot stand in for
   this R2-only acceptance.
5. Perform this recipe on the selected native Linux build, independently of local
   macOS reader/editor receipts. Update compatibility/docs or implement only an
   actual missing storage seam revealed by the recipe. Release/publication and
   production deployment remain separate work.

### Approved local retry boundary — 2026-11-12

The user explicitly approved restarting Docker Desktop, cleaning only the prior
test containers and retrying the same smoke once. Docker restarted gracefully;
the two verified test containers were removed and the unused cold-container name
was absent. Both old containers reported `OOMKilled: true` after recovery; this
is new evidence without a complete causal attribution for the earlier observer
failure. Preserve that failed run, receipt and artifacts unchanged. The retry uses
the same source-verified immutable image, platform and assertions, fresh names and
ports, an early private artifact-path record, and passive lifecycle/OOM events
filtered to its own containers. No memory-setting change, reset, prune, model
inference, cloud operation or deployment is included. A second retry is outside
this approval.

## Later read and edit acceptance

Live browsing must attach without stealing the owner session or modifying the
workspace, enforce configured roots/private-path exclusions, and release leases on
close/expiry/process death. Saved browsing must remain stable across later edits,
verify corruption even outside a requested range, and coordinate with pruning.
Consumer authorization is independent of a resource URI or registry team label;
these local read profiles do not certify protected tenant ACLs.

Human editing is later and opt-in. Require a qualified public API/artifact, stale
basis rejection, lost-reply resolution without another payload write, explicit
unknown outcomes and proven quiescence of every outside writer. No simultaneous
collaborative editing or durable recovery claim follows from private C.2 publication.

These requirements retain native local/Cloudflare-container execution. They add no
FUSE requirement, Wasm POSIX runtime, Workers storage adapter, executor or agent loop
to Stow's scope. Existing W27/W28 and platform-profile gates remain conditional.
