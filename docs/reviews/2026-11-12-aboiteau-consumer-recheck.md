# Aboiteau cached-image consumer recheck

## Result

The local full consumer smoke is failed/inconclusive, not a pass. It used the
source-verified cached image `sha256:8ae47350f2de6630c1dad9625fd686898c685855ca7f8da8689a4bf09cd09284`
(`linux/amd64`, Stow 0.3.7, binary SHA-256
`780c3ccf89abdb4355e1b943ab686693d7436bd08cefdffd0f315226815f27da`) under
QEMU on the local aarch64 Docker daemon. The preflight matched all 12 Docker
`COPY` inputs; `control-server.ts` was the B2-proven source hash. No consumer
source was edited, no provider/model inference ran, and no Cloudflare/R2 operation
was invoked.

The smoke reached 21 successful checks, including actual controller startup and
native Workspace readiness, UID/binary verification, direct-file/S3 byte and
content-type parity, workspace and state checkpoints/restores, same-identity
workspace recovery, missing-registry refusal, and account-seed archive isolation.
The account-seed operation and concurrent `/start` both returned HTTP 200. The
race helper then failed while taking its final container-state observation,
before it emitted the final lifecycle booleans. This is not a completed race
assertion and does not establish a product lifecycle defect.

Separately, the 32 host consumer contract tests passed with 105 assertions across
four files. They do not replace the failed full-container smoke.

The observer runs `docker exec ... bun ... native-lifecycle-observe` with a
five-second timeout and collapses execution, timeout, JSON and result-shape
failures to a generic `Error`. The retained log therefore cannot distinguish an
observer/emulator delay from a failed observation. Later Docker info/ps and Unix
socket probes also timed out, while the socket existed; the cause is unknown.
The smoke's EXIT diagnostics hung, and the parent terminated its dedicated shell
group 39944 (exit -15 after 618,137 ms). Docker inspection timed out for all three
owned names, so removal was not attempted and cleanup could not be confirmed. The
main container `stow-proof-m27hjpkw` and account-seed container
`stow-proof-m27hjpkw-account-seed` may remain. The `-cold` name was not created;
the cold stage was not reached. These names and retained files are recorded in the
[evidence receipt](../assessment-evidence/2026-11-12/aboiteau-consumer-recheck.json).

The result is limited to this local emulated run. The earlier native-amd64 CI B2
smoke and C2 R2 recovery remain prior consumer-reported evidence; this attempt
did not repeat them. No new failure cause is assigned. Any follow-up should first
verify Docker cleanup/health and preserve the unchanged lifecycle assertions;
distinguish observer timeout/error from a returned-but-false lifecycle result.
