# Workspace uncertainty qualification evidence — 2026-10-19

This directory holds the receipts for the
[workspace uncertainty correction](../../reviews/2026-10-19-workspace-uncertainty.md),
which closes C04 from the
[bounded closure register](../../reviews/2026-10-18-correctness-closure.md).

| Path | Contents |
|---|---|
| `initial/` | The red reproduction: the pre-fix revisions of the changed files, run through a Go overlay so the failure reported is the product's answer. |
| `final/` | The green run: the corrected code under the same plan. |
| `plan.json` | The steps the runner executed, in order. |
| `before-overlay.json` | The overlay that restores the pre-fix revisions. |
| `closure_probe_test.go.txt` | The self-contained red probe, kept as text so it can be read without the Go tree. |
| `run.json` | The exit code and expected exit code of every step. |
| `export-receipt.json` | The SHA-256 of every exported file. |
| `qualified-source.json` | The SHA-256 of every source file that was qualified. |
| `transport-receipt.json` | The archive that carried the source to the runner. |
| `runner-stop-status.json` | The state of the scratch deployment after the run. |
| `SHA256.json` | The SHA-256 of every file in this directory. |

All execution ran on the Railway scratch runner with the test steps running as
an unprivileged user, because a root process can read a file with no
permissions and the reproduction would then pass without proving anything.
