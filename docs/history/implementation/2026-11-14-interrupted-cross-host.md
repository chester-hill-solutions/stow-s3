# Interrupted cross-host workspace continuation

**Result:** a fresh Luna context continued committed workspace state from a
macOS ARM64 sender to a separate native Linux AMD64 receiver, both using the
published Stow v0.4.1 binary. The sender model was deliberately interrupted
while its next file read was held at the host proxy. The host stopped the model-facing server, recovered the
already committed save, checkpointed, removed the source
workspace/registry/tree, and transferred the selected workspace checkpoint.
The receiver adopted it and completed the task from a fresh model context.

The [structured receipt](./2026-11-14-interrupted-cross-host.json) records the
run and host/model evidence. The run occurred locally on November 14; the
timestamped model and host receipts use UTC on November 15.

## Artifact and host profile

The release source was `7364b1d84276f32760f1ab10dfaf0825f270289c`. Both native
artifacts reported Stow `0.4.1`, Go `1.25.6`, `CGO_ENABLED=0`, and that source
revision with clean VCS metadata. The Darwin ARM64 sender binary SHA-256 was
`d90d94fbde3af98093e9c67983beffe522d7d76408cd2dcf0ab454b3fe7c8901`; the Linux
AMD64 receiver binary SHA-256 was
`764cf2887d41308b89165ac0a151220c845fcacca872f52d97165ec6509f8705`.

The sender ran on macOS ARM64 / Apple M1 Pro with Node `24.22.3`. The receiver
ran on a separate Linux x64 AMD EPYC 9655P host with Node `25.9.0`, a CPU ceiling of four vCPUs
and a 4 GiB memory limit. Both used the official Node MCP SDK `1.32.1`. Each
stage used a fresh `gpt-6-luna` context at medium reasoning, with no inherited
turns. The receiver received neither the sender transcript nor an expected
answer.

## Interrupted sender and host recovery

The synthetic task began with `docs/STATE.json` at 11 units across 2 entries,
with `docs/batch-a.csv` next. The sender read that state and batch A, then
committed `docs/STATE.json` at 26 units across 5 entries with batch B next. The
host verified the matching save receipt and replayed resolution: request key
`cf9ac7fc-1ea7-42ef-9671-a58f39e9ad93`, generation
`ba1c81ddd3c34c93e75c3d2c7419ec92`.

Before forwarding the sender's next read of `docs/batch-b.csv`, the proxy held
the request. The model agent was still running when interrupted; the subsequent
agent-status observation was `interrupted`, and no model terminal result
existed. This is an interruption after a durable save and before the next
read. Mid-save interruption and storage faults are separate profiles.

The host then closed the proxy and stopped the native server, which exited
cleanly. With explicit `--recover-file-saves`, it resolved the committed save,
created and verified the source checkpoint, and exported the selected
workspace. The source workspace, registry, and staging tree were absent before
receiver adoption. The selected payload comprised only `docs/STATE.json`,
`docs/batch-a.csv`, and `docs/batch-b.csv`.

## Fresh receiver continuation

The Linux receiver adopted the checkpoint under a new workspace identity. Its
fresh Luna context read the saved state and batch B, then committed
`docs/STATE.json` at 56 units across 7 entries with no next batch. It made
exactly two reads and one save: request key
`8128bf13-0419-48d7-84ec-98be58770548`, generation
`c30b45a7673b6e43a906c3658573de17`. The host resolved the save as a replay and
verified the actual checkpoint state: 52 bytes with SHA-256
`8beb16d5259e655251b0011de89722302bce0170604c2233ee01ea15d3c34123`. The
committed save generation matched its replayed receipt; checkpoint bytes matched
that save.
The source checkpoint was `cp_880abd24e43c300a424a5a48`; the receiver adopted
it under workspace `ws_ed1dcfe0766db5dc` and wrote checkpoint
`cp_aea840ae7acb4756c5d9d99c`.

This was one actual model pair with no save retry. Scripted transport smoke
remains separate from model evidence. The [evidence bundle](https://github.com/chester-hill-solutions/stow-s3/releases/download/v0.4.1/stow-v0.4.1-cross-host-continuation-evidence-20261114.zip)
is attached to v0.4.1 and was anonymously downloaded and checksum-verified. Its
SHA-256 is `b0284deb3c4ad8d8da5820beecbce7738bc6e907cc6470facd187ad6af967a75`.
It includes audited calls, exact synthetic checkpoint bytes and sanitized
reproduction templates. Host routes and paths are configurable; template hashes
are distinct from the original executed-source hashes. The checkpoint archive
is unchanged. The receiver deployment was confirmed stopped after export.

## Scope

This demonstrates a published-binary, cross-host native workspace continuation
with a deliberate model interruption after a verified save. It does not update
or requalify the Aboiteau integration: its earlier B2 native-amd64 and C2 R2
receipts remain separate, and its local QEMU recheck and retry remain failed.
There were no Cloudflare/R2 changes or OpenRouter credential loads in this run.

The result does not establish an OS sandbox, protected or multi-tenant
authorization, power-loss behavior during a save, or the two-team repeat-use
evidence required by W22. It is not evidence for OpenCode/Free Router
compatibility. The receiver model read an adopted mutable workspace; resource-only
saved-view model access on a second host remains open. Stow product APIs are
unchanged; execution and coordination stayed in caller-owned test scripts.
