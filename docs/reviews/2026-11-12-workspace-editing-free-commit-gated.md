# Free Router commit-gated workspace editing attempt

**Result:** The live task saved the correct state in both stages, but the strict
trace gate rejected stage two. This is not a qualified OpenCode / Free Router
editing pass. The [receipt](../assessment-evidence/2026-11-12/workspace-editing-free-commit-gated.json)
retains the failed status; separate host follow-up evidence records post-run
verification without another model prompt or save.

The user authorized one new Free Router attempt after the caller added three
phases: force an initial STATE read without a response schema, require tools
without terminal schema while editing, and enable a no-tool terminal schema only
after a correlated committed save. The final generation field was constrained
to the generation returned by that save. Two task prompts completed in 63,089 ms;
all eight completion requests used `openrouter/free` and returned HTTP 200, with
no paid fallback or automatic task retry.

Stage one passed strict verification. It read STATE and `docs/batch-a.csv`, saved
26 units / 5 entries, and produced generation
`3b8efe57a84e39b27f4673f23c5bec37` and checkpoint
`cp_68e2ed9aaa05eba2961e8ec3`. The source workspace, registry and staging tree
were removed before stage two continued from the adopted checkpoint.

Stage two read STATE, `docs/batch-b.csv`, and an extra `docs/batch-a.csv` before
making one save. Its calculated state was 56 units / 7 entries with no next
batch; the save and terminal result both used generation
`0461e3e2cfd541a83bcdbf03452fe7e0`. The verifier rejected the run for the extra
read: **“editing trace read a path outside the stage inputs.”** The path was
inside the configured `docs` root, but it was not an allowed stage-two input.
The verifier and its allowlist were not changed to accept it.

After the failed pilot, an independent host resolved the existing request and
verified a committed replay for the same path, key and generation. It read back
the exact 52-byte state with SHA-256
`8beb16d5259e655251b0011de89722302bce0170604c2233ee01ea15d3c34123`, created
checkpoint `cp_54fb5afab2697663e93243e3`, and confirmed the checkpoint bytes
matched. Both CSV inputs remained
unchanged. This confirms the persisted save and resulting state; it does not
override the failed trace qualification. No additional model prompt or save
occurred, and all earlier receipts remain unchanged.

Before the live attempt, one mock run failed before any provider request; a
separate local 502 retry was retained, followed by a fresh 9.4-second mock pass
with eight fake completion requests. The 502 was not reproduced in that fresh
pass. These mock outcomes do not affect the live verdict.

This result is limited to OpenCode v2.2.7, the local Free Router profile, and the
same macOS ARM64 host. Direct Luna MCP continuation remains separately evidenced
in the [direct profile report](2026-11-12-workspace-editing-luna-direct.md).
Linux, second-host, protected-policy and deployment gates remain open.
