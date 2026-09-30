# Native object MCP contract

**W02 increment, 2026-09-30.** Implemented; local verification is recorded in the
[dated receipt](implementation-2026-09-30.md#native-object-mcp-wrapper-addendum).
This is a reference for the [consolidated plan](storage-foundation-plan.md), not a
second work queue. It wraps the [qualified native filesystem save core](portable-workspace-usage.md#filesystem-save-receipts-and-recovery)
for MCP callers. The existing workspace/checkpoint MCP profile keeps its contract.

## Owned profile

The host launches the existing native binary with one object directory and one
bucket. Tool inputs contain object keys, not directories, backend selectors,
bucket selectors or authority. A missing bucket requires explicit startup creation
consent. A read-only profile can read and resolve retained saves; it cannot save or
create a missing bucket. The runtime owns the directory until MCP EOF/shutdown;
closing releases ownership and preserves object data and save receipts.

This profile uses object records, not the editable workspace directory/manifest
format. It does not qualify arbitrary host edits, shared-host ownership, resource
ACLs, encryption or historical object-body retention. The host owns process access
and caller identity; a fixed bucket selector is not a general resource ACL system.
The MCP SDK negotiates its existing JSON-RPC protocol. The future generated
Protobuf content-journal envelope remains W08; this adapter adds no new WASM wire
protocol or independent object model.

## Read, save and resolution

The object profile exposes read-for-save, keyed save, save resolution, observation
release and capability tools. A read returns coherent bytes, logical metadata and
an observation token, including explicit observed absence. The server retains the
actual opaque native save condition; it does not serialize a native condition or
reimplement comparison in an adapter. Tokens bind to the current server/runtime
and exact key. Exactly one token or deliberate `replace: true` is required for a
save. Legacy conditions and caller-supplied bucket/path selectors are refused.

Before calling save, the caller persists a unique request key and the intended
resource, data and options. Save requires that key. Repeating a live request keeps
its original token and complete meaning; core replay returns the original result
without overwriting later contents. A changed meaning refuses. The caller releases
the token when it no longer needs it. Release affects only an observation, never
object data, receipts or retention.

Tokens expire after 15 minutes and are reaped on access. They do not survive server
close/reopen. After an uncertain reply or restart, call resolution with the original
object key and request key. It does not retry an intended body. Missing evidence is
unknown. A fresh read is a new observation, not proof that the original save failed.
Receipts retain metadata; a later ordinary fetch returns current bytes.

The object tools use an independent version-1 result envelope. The effect outcome
(`committed`, `not_committed`, `unknown`) remains separate from an error reason and
MCP `isError`. A known commit with a response/cleanup error stays committed. An
oversized post-effect result returns a minimal envelope preserving its effect.
`available` means the requested read or receipt information is included; it does
not promise that a historical object body remains available. Read, release and
capability tools use `ok` as their non-effect outcome.
MCP schema, transport or cancellation failures without a valid effect envelope do
not establish absence of a previous effect. Callers treat those as unknown for a
keyed save and resolve before starting another mutation.

## Bounds and evidence

The profile limits decoded tool object bodies to 64 KiB, observations to 256, admitted
tool calls to eight, tool names to 128 bytes, raw tool arguments to 256 KiB, and encoded full tool results
to 256 KiB. Hosts may narrow body
and observation limits. Observation tokens expire; durable save receipts do not
expire automatically. Native receipt count/byte limits and object/multipart quota
admission continue to apply. `retention_full` identifies receipt capacity exhaustion;
deleting objects does not release retained receipts. Busy/full requests refuse rather than creating an
unbounded operation queue or silently evicting another observation.

The native stdio transport limits incoming lines to 1 MiB. Tool-result accounting
includes both structured and matching JSON text forms. These bounds do not promise
a process RSS ceiling or a bound on arbitrary JSON-RPC envelope IDs/parser state.
The filesystem backend may materialize records while checking their size. Object
body limits describe adapter payloads, not streaming storage or physical disk
preallocation. No limit silently changes the save's core outcome.

Local verification covers the real SDK and built binary, schema/selector refusals,
stale and absence/replacement saves, same-key replay, reopen resolution, read-only
refusal, token expiry/release/capacity, payload/result limits and owned shutdown.
A discarded reply followed by reopen is synthetic recovery evidence, not an
executed power-loss test or two real downstream consumer integrations.
