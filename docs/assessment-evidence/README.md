# Assessment evidence

These are dated probe methods and test receipts. Use the
[documentation index](../README.md) for current product behavior.
Bulk logs, probes and source receipts are preserved in the
[evidence archive](../evidence-archive.md).

| Date | Evidence |
| --- | --- |
| November 12, 2026 | [Aboiteau cached-image retry receipt](2026-11-12/aboiteau-consumer-retry.json): 21 checks passed before the lifecycle helper failed prior to streaming; OOM event observed and retry cleanup verified |
| November 12, 2026 | [Aboiteau cached-image consumer recheck receipt](2026-11-12/aboiteau-consumer-recheck.json): 21 checks passed before final container-state observation failed; Docker cleanup remains unconfirmed under local amd64 emulation |
| November 12, 2026 | [Direct Luna checkpoint continuation receipt](2026-11-12/checkpoint-luna-continuation.json): whole JSON and seven successful resource calls verified against the adopted saved checkpoint; same-machine read-only model evidence |
| November 12, 2026 | [Workspace editing proxy repair receipt](2026-11-12/workspace-editing-proxy-repair.json): bounded local error telemetry and corrected paid-fallback accounting; F.5 remains failed and unqualified |
| November 12, 2026 | [Scope-aligned Free Router editing receipt](2026-11-12/workspace-editing-scope-aligned.json): one correct stage-one save, but strict verification rejected local proxy 502/retry history; stage two did not run |
| November 12, 2026 | [Free Router commit-gated editing receipt](2026-11-12/workspace-editing-free-commit-gated.json): saves committed in both stages, but stage-two extra read was outside the allowed inputs and strict verification rejected the trace |
| November 12, 2026 | [Repeat direct Luna workspace editing receipt](2026-11-12/workspace-editing-luna-repeat.json): two fresh stages passed; source was removed before checkpoint continuation, with exact receiver-state bytes verified |
| November 12, 2026 | [Direct Luna workspace editing receipt](2026-11-12/workspace-editing-luna-direct.json): both model stages completed three native MCP file calls each; source was removed before fresh-context checkpoint continuation on the same host |
| November 12, 2026 | [Second failed live workspace editing model receipt](2026-11-12/workspace-editing-model-required-read.json): correlated initial STATE read succeeded, but no batch read/save followed; all inputs remained unchanged |
| November 12, 2026 | [Failed live workspace editing model receipt](2026-11-12/workspace-editing-model-local.json): strict gate rejected a schema-valid terminal response with no MCP calls; inputs remained unchanged |
| November 12, 2026 | [CLI/MCP editing and SDK receipt](2026-11-12/workspace-editing-mcp.json) and [E.1/E.2 verification](../reviews/2026-11-12-workspace-editing-mcp.md) |
| November 11, 2026 | [Native workspace save recovery receipt](2026-11-11/workspace-editor-recovery.json) and [D.2 verification](../reviews/2026-11-11-workspace-editor-recovery.md) |
| November 11, 2026 | [Internal durable save-request receipt](2026-11-11/workspace-editor-durable-requests.json) and [D.1 verification](../reviews/2026-11-11-workspace-editor-durable-requests.md) |
| November 11, 2026 | [Internal editor publication receipt](2026-11-11/workspace-editor-publication.json) and [local C.2 verification](../reviews/2026-11-11-workspace-editor-publication.md) |
| November 11, 2026 | [Native editor observation receipt](2026-11-11/workspace-editor-observations.json) and [local C.1 qualification](../reviews/2026-11-11-workspace-editor-observations.md) |
| November 11, 2026 | [Corrected local checkpoint model attempt](2026-11-11/checkpoint-model-local-attempt2.json): all reads valid, strict terminal format failure retained |
| November 11, 2026 | [Failed local checkpoint model attempt](2026-11-11/checkpoint-model-local.json): correct saved-input answer, strict trace failure retained |
| November 11, 2026 | [Local checkpoint adoption receipt](2026-11-11/checkpoint-local.json) and [qualification](../reviews/2026-11-11-checkpoint-resources.md) |
| November 11, 2026 | [Workspace resource host receipt](2026-11-11/workspace-resources-host.json) and [qualification](../reviews/2026-11-11-workspace-resource-hosts.md) |
| September 29, 2026 | [Assessment methods](2026-09-29/README.md) and [implementation methods](2026-09-29/implementation/README.md) |
| October 12, 2026 | [Railway browser run](https://github.com/chester-hill-solutions/stow-s3/blob/e549d047b3b5aa220563a486deb4e7aa8caa4a19/docs/assessment-evidence/2026-10-12/railway-browser.json) |
| October 14, 2026 | [Release receipts](https://github.com/chester-hill-solutions/stow-s3/tree/e549d047b3b5aa220563a486deb4e7aa8caa4a19/docs/assessment-evidence/2026-10-14-release) |
| October 15, 2026 | [Patch receipts](https://github.com/chester-hill-solutions/stow-s3/tree/e549d047b3b5aa220563a486deb4e7aa8caa4a19/docs/assessment-evidence/2026-10-15-patch) |
| October 18, 2026 | [Release receipts](https://github.com/chester-hill-solutions/stow-s3/tree/e549d047b3b5aa220563a486deb4e7aa8caa4a19/docs/assessment-evidence/2026-10-18-release) |
| October 19, 2026 | [Reconciliation receipts](https://github.com/chester-hill-solutions/stow-s3/tree/e549d047b3b5aa220563a486deb4e7aa8caa4a19/docs/assessment-evidence/2026-10-19-reconciliation) |
| October 19, 2026 | [Workspace uncertainty regression](2026-10-19-workspace-uncertainty/README.md) |
