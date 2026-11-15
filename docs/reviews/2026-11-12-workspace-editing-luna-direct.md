# Direct Luna workspace editing and continuation

**Result:** Positive for the local native MCP editing flow on the same macOS ARM64
host, 2026-11-12. This is a separate caller profile from the two failed OpenCode /
OpenRouter Free Router runs. The [receipt](../assessment-evidence/2026-11-12/workspace-editing-luna-direct.json)
and retained private trace record both stages.

Two fresh `gpt-6-luna` agents at medium reasoning (`fork_turns: none`) each drove
the official MCP SDK against a native Stow HTTP server. Each stage exposed only
the four workspace editing tools through the audited loopback endpoint. The
first agent read `docs/STATE.json` and `docs/batch-a.csv`, then saved the updated
state. Host resolution returned the same committed save with `replayed: true`;
the host confirmed file generation `aadcfd1a5a98cf040d3127b6144c6054` and captured
checkpoint `cp_de9d5517d086d20f08128fe1`. Its result was total 26 units across 5
entries, with `docs/batch-b.csv` next.

The source workspace, registry and staging tree were removed before handoff to
the second fresh agent. The host adopted the checkpoint under a different workspace
identity. That agent received only the receiver endpoint, its request key and
the generic task rule. It read the saved state and `docs/batch-b.csv`, saved the
next state, and the host resolved the commit as a replay. The resulting state
was 56 units across 7 entries. Checkpoint
`cp_f8fd2b88f82c61f530e61bd5` was read back as 52 exact bytes; its SHA-256
`8beb16d5259e655251b0011de89722302bce0170604c2233ee01ea15d3c34123` matched
the saved state bytes.

The first attempt to connect the NodeREPL directly to the native MCP endpoint
failed with `EPERM` before an MCP call and had no effect. The parent switched
the same agent to a fixed official-SDK CLI bridge; this was setup recovery, not
a save retry. A separate scripted SDK smoke is not counted as model evidence.
The live run completed two stages, three model MCP calls per stage, and exited
successfully within the five-minute limit. No OpenRouter request or credential
access occurred, and the earlier failed OpenRouter receipts remain unchanged.

This demonstrates model-directed reads and saves through the native MCP file
tools, followed by fresh-context checkpoint continuation on one physical host.
It does not establish a process-wide filesystem sandbox: the generic Node / exec
runtime retained host capabilities even though the MCP proxy allowed only the
four tools. The OpenCode / Free Router profiles remain unqualified. Linux,
second-host, protected-policy and deployment gates remain open.

## Repeat run

A second fresh-root run repeated the same direct MCP flow with two new
`gpt-6-luna` agents. Both stages made exactly three MCP calls (two reads and one
save), and host resolution replayed each matching committed result. Stage one
generation was `5148d9dd9cc12267878082af41e6b9dd`; stage two was
`8c6525994073fbc454d23d5532ea3106`. The source workspace, registry and staging
tree were removed before the host adopted the stage-one checkpoint and started
the fresh second context. The final 52-byte checkpoint SHA-256 matched both the
saved bytes and the receiver file. The artifact time span was 249,022 ms, within
the five-minute limit. See the [repeat receipt](../assessment-evidence/2026-11-12/workspace-editing-luna-repeat.json).

Two shell-quoting setup failures in the initial stage-one context occurred before
any MCP call. The parent supplied a base64url argument codec; the same agent then
continued. These were setup recoveries, not save retries. The previous five
receipts and native/caller production sources remained unchanged. This repeat
does not expand the original same-host qualification or its isolation limits.
