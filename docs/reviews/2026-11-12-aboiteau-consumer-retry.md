# Aboiteau cached-image consumer retry

## Result

The one authorized retry failed before the account-seed/start lifecycle stream
began. It does not pass the full consumer smoke and does not establish a product
defect. It used the same source-verified cached `linux/amd64` image under QEMU on
the local aarch64 Docker Desktop daemon. The current image source hashes matched
all 12 Docker `COPY` inputs. No consumer source, model, Cloudflare/R2 resource or
deployment was changed or invoked.

The unchanged smoke assertions reached 21 successful checkpoints before the
lifecycle helper returned an error in its `validate-arguments` phase. In the
helper, the initial `observe()` runs before the phase advances; its failure can
therefore report `validate-arguments` even when invocation arguments are valid.
No seed or `/start` request statuses were recorded, and no final lifecycle
booleans were emitted. A passive Docker event recorded an OOM event on the main
container. Later, the main and account-seed containers exited with status 137
during cleanup; the available evidence does not attribute both exits solely to
that OOM event. The cold-container phase did not run.

Docker Desktop was restarted with explicit user authorization. Before the retry,
the old run's main and account-seed containers were confirmed removed after
inspection showed them OOM-killed; its cold name was absent. After the retry, its
owned test containers were confirmed absent. Cleanup removed only the test
containers. The [first recheck report](2026-11-12-aboiteau-consumer-recheck.md)
preserves its earlier, as-of-run cleanup uncertainty; this retry resolves cleanup
without changing that record. The [evidence receipt](../assessment-evidence/2026-11-12/aboiteau-consumer-retry.json)
contains the bounded run and cleanup observations.

The local contract suite separately passed 32 tests with 105 assertions across
four files. The prior native-amd64 B2 CI and C2 R2 recovery remain separate
historical evidence; this QEMU attempt did not repeat them. No further smoke run
or implementation change was made. A native-amd64 or better-provisioned recheck
is a candidate next step under separate scope. No memory settings changed;
additional memory is not a verified fix.
