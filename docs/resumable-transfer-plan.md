# Resumable upload and download validation plan

> **Consolidated on 2026-09-29; reference only.** The [new working-storage plan](storage-foundation-plan.md) is the sole work order. Same-store continuation is W06 and consumer integration W21. The retained recipe is a design reference, not a separate queue. Former checklists, phase numbers and next steps below do not schedule work.

**Status:** proposed validation and consumer integration, 2026-09-29. This
expands S0-4, S1-5 and S4-1 in the [canonical plan](plan.md). The user clarified
that resumability means continuing interrupted file transfers. The earlier
[task continuation plan](task-resumability-plan.md) is retained as superseded
conversation history and does not schedule work.

The goal is to interrupt a file upload or download, restart the client, and
transfer only the remaining data while verifying the complete result. Start
with Stow's existing S3 operations. Add code only where a real client workflow
exposes missing behavior or needs a small consumer-owned progress record.

## Existing support

| Mechanism | Source evidence | What the client must do |
| --- | --- | --- |
| Multipart upload | S3 initiation, part upload/listing, completion and abort handlers exist. Filesystem and workspace backends persist upload metadata and part bytes. | Retain the upload ID, bucket/key, source identity and part layout; list acknowledged parts after restart and upload missing parts. |
| Partial download | GET supports byte ranges, partial-content responses and conditional read validators. | Retain verified local bytes and object identity; request the remaining range with an unchanged-object condition and validate the response. |
| Persistent storage | On-disk backends can reopen retained state. | Reconnect to the same retained store using current credentials. Memory-backed or deleted temporary stores cannot retain an upload across server restart. |

Relevant implementations are [filesystem multipart](../internal/storage/fs/fs_multipart.go),
[workspace multipart](../internal/storage/workspace/multipart.go),
[S3 multipart handlers](../internal/s3api/handlers_multipart.go), and
[object GET](../internal/s3api/handlers_bucket.go). The
[compatibility contract](compat-contract.md) documents range and multipart limits.

These mechanisms are not proof that every SDK's high-level upload/download
helper automatically resumes after its process dies. Stow's language wrappers
provide storage access; a durable transfer-progress helper was not identified
in the inspected package sources. Qualify specific client behavior rather than
claiming automatic resume.

Incomplete multipart staging is excluded from portable workspace checkpoints.
Moving a checkpoint therefore does not move an in-flight upload. The first
scope is client restart and server restart against the same retained backend,
not moving an incomplete upload to a different storage service.

The subsequent [portable recovery decision](portable-recovery-plan.md) accepts
cross-host data plus selected recovery state as an S1-9 extension. This initial
same-store validation remains a prerequisite; it does not yet define portable
part payloads, transfer ownership or destination activation.

## Ordered work

1. **Prove upload continuation using the ordinary S3 SDK.** Upload a small
   synthetic file in multiple parts. Persist its upload ID and source identity,
   stop after acknowledged parts, and start a fresh client. Use ListParts to
   reconcile server state, send only missing parts, complete the object, and
   independently verify exact bytes. Repeat after restarting Stow with the same
   data directory. Report supported backends separately.
2. **Prove download continuation using range GET.** Download and durably retain
   a prefix, then stop the client. Start a new client and request bytes beginning
   at the durable local offset, with If-Match against the recorded ETag. Verify
   status, Content-Range, identity and total size before appending. Compare the
   final bytes with the original fixture. Never append a full 200 response as
   though it were the requested suffix.
3. **Exercise failure cases.** Cover an interrupted part, a lost part reply,
   changed local source, overwritten remote object, missing/aborted upload,
   expired signed link, corrupt/truncated local progress and a lost completion
   reply. Resolve ambiguous completion by checking the final object against the
   expected content; do not treat a missing upload ID as proof of success. ETags
   are validators and multipart ETags are not whole-file MD5 digests.
4. **Measure the supported workload.** Record resent bytes, memory, disk usage,
   part sizes and elapsed time under explicit limits. Stow currently buffers
   upload/completion data and is intended for development/testing; do not infer
   production-scale media suitability from a small fixture. CRC64NVME multipart
   is explicitly unsupported; select a supported checksum profile deliberately.
5. **Pilot one actual consumer.** Start with cococlips, which already transfers
   media through its S3 storage adapter. Its current file upload uses a single
   PutObject and its download buffers the full body, so resume would require a
   consumer integration rather than a Stow storage rewrite. Implement a small
   persistent transfer record only after the SDK exercise establishes what is
   needed. Use short synthetic clips first. pg_backup is a second candidate for
   uploads; its current upload also uses a single PutObject.
6. **Document the verified recipe.** Explain which client and backend were
   tested, what survives a restart, how expired credentials/links are renewed,
   and how cancel/abort differs from pause. Add package convenience APIs only
   if repeated consumer use demonstrates a common unmet need.

## Transfer progress and safety

The consumer owns progress and retry decisions. An upload record needs the
destination, upload ID, stable source fingerprint, part layout and reconciliation
information. Server listing is authoritative for acknowledged parts; a lost
response may mean the part already arrived. Reuse the same part number when
retrying the same bytes. A changed source must not be silently mixed with parts
from the original file.

A download record needs object identity/size, destination and a durable local
offset. Persist downloaded bytes before advancing that offset, and verify the
existing prefix after restart. A changed object requires an explicit restart or
failure. An expired link should be renewed for the same object identity, not
treated as evidence that the stored prefix is invalid.

Use the existing SDK and backend APIs. Keep credentials outside portable progress
files. Stow needs no agent notes, task schema, scheduler or new transfer protocol
for the initial proof. Any reproduced protocol defect belongs in the existing
storage/S3 implementation; client progress handling belongs in the consumer.

## Acceptance and verification

Pass requires exact final bytes, preserved supported metadata, continuation from
acknowledged parts or a verified local prefix, and evidence that acknowledged
data was not sent again. An incomplete current part may be retransmitted. Source
or destination changes must be detected rather than produce a mixed file.

Extend the current multipart, range and real-SDK conformance tests only for
uncovered restart/continuation cases. Record client-only restart, retained-server
restart and memory-state loss separately. A same-store restart pass does not
prove cross-host transfer-state portability. Run focused checks for changed
code; existing release gates remain in force before publication. This document
records proposed work, not a completed transfer-resume test.
