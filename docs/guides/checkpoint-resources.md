# Read saved checkpoint files

This native development profile exposes one selected checkpoint through MCP
resources. It serves captured filesystem files from v1 and v2 checkpoints.
The [workspace resource guide](workspace-resources.md) covers current working files.

**Availability:** this is an unreleased 0.4.0 candidate profile. It requires a
binary built from this checkout; published v0.3.7 binaries do not include these
flags or resources. See the [candidate notes](../releases/v0.4.0.md).

Build this checkout with `make build`. Pause writers and use the existing
`workspace checkpoint` command to capture saved work, then select the returned
checkpoint ID:

```sh
/absolute/path/to/stow-s3 workspace checkpoint \
  --registry-dir /absolute/registry --id WORKSPACE_ID

/absolute/path/to/stow-s3 mcp \
  --registry-dir /absolute/registry --workspace-id WORKSPACE_ID \
  --workspace-files --checkpoint-id CHECKPOINT_ID --resources-only \
  --read-root inputs --timeout 30s
```

The checkpoint must belong to the selected registered workspace. Its working
files can subsequently change or be absent; saved reads still verify the captured
bytes. Omit `--read-root` for the permitted whole file view, or repeat it for up to
16 distinct existing directories in the saved file tree. Mandatory sensitive-path
exclusions always apply. Empty directories and v2 logical object payloads are
outside this file view.

Resource-only mode has no tools or prompts. Normal mode retains the existing
checkpoint tools and their grants independently of file-resource roots.
The saved profile does not enable the browser. Existing mutable resource URLs
retain their original meaning and are unavailable through this selected profile.

## Browse and read ranges

Start at `stow://workspaces/WORKSPACE_ID/checkpoints/CHECKPOINT_ID/v1`.
Read its `root_uri`, follow directory entry URIs, and follow `page.next_uri`
unchanged until absent. The directory tree comes from the retained manifest,
with stable ordering throughout this attachment.

For a file, `uri` reads complete contents up to 64 KiB. Its `range_uri` starts a
window of up to 64 KiB. A range returns JSON containing workspace/checkpoint
identity, the manifest digest, path, full file `size` and `sha256`, offset, actual
length, either UTF-8 `text` or `blob_base64`, and optional `next_uri`.

Decode the selected representation into bytes, append successive windows in
offset order, and follow `next_uri` unchanged. After reconstruction, verify total
length and SHA-256 against the returned full-file metadata. A window at EOF can
contain zero bytes; an empty string remains a valid representation.

Every window verifies the **entire saved file** before returning bytes, while
retaining only the requested window. Corruption outside that window also refuses
the read. This verification work repeats for each range, so full reconstruction
costs file size multiplied by the number of requests. Files over 64 MiB refuse;
larger-file indexing and cheaper partial integrity verification remain future work.

| Bound | Value |
| --- | --- |
| Manifest decoded bytes | 4 MiB for this reader; legacy `LoadCheckpoint` remains 16 MiB |
| Manifest files / charged metadata | 10,000 / 4 MiB |
| Directory page / complete encoded response | 100 entries / 256 KiB |
| Whole file / requested range | 64 KiB / 64 KiB |
| Full-file verification per range | 64 MiB maximum |
| Concurrent resource reads / request deadline | 8 / min(configured timeout, 10 seconds, view expiry) |
| Attachment lifetime | Five minutes, absolute |

## Attachment lifetime

Shared OS leases prevent participating Stow deletion paths from removing the
selected checkpoint while attached. Capture uses separate locks and can continue.
An expiry timer closes an idle reader, and process death releases both leases.
This uses no durable recovery-hold rows. Close and expiry preserve checkpoint data;
they release retention protection rather than deleting saved work.

The overview returns `expires_at`. Reconnect to open a new bounded view after
expiry. A checkpoint deleted between attachments is unavailable. All deletion
participants must use a version supporting these checkpoint-reader leases.
Filesystem calls cannot be forcibly preempted by the reader deadline.

## Continue locally

Reuse verified checkpoint export and adoption. The receiving workspace has a new
local ID; launch a new resource server using that returned ID. Imported source
checkpoint identity and receiver working identity remain distinct. Existing
connections do not silently expand to the receiver.

Installed native consumers on Linux and macOS, x64 and ARM64, verify saved
bytes after changing the live file. See the [candidate notes](../releases/v0.4.0.md)
for revision-bound release evidence. Cross-machine continuation, named model
providers and protected saved-version access policy require separate evidence.
This profile remains read-only; managed editing uses its own connection.

<!-- repo-only:start -->
The local qualification exercises source removal, adoption and byte verification
on this machine. Cross-machine continuation and named model-provider acceptance remain separate
from the native platform release checks. [Local model attempts](../reviews/2026-11-11-checkpoint-resources.md#corrected-fresh-context-attempt)
returned the correct saved-input result. The corrected run completed all resource
reads but failed the strict terminal JSON format gate. These file-resource roots do not claim protected saved-version
access policy or guarded editing.

<!-- repo-only:end -->

The [contract](../contracts/checkpoint-resources-contract.md) defines identity,
integrity and retention.
<!-- repo-only:start -->
The [canonical plan](../development/storage-foundation-plan.md) owns local and
cross-host qualification status.
<!-- repo-only:end -->
