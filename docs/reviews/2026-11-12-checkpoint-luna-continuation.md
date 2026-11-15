# Direct Luna checkpoint continuation

## Result

The local immutable-checkpoint continuation passed on one macOS ARM64 host.
A fresh `gpt-6-luna` / medium agent with no inherited turns used the receiver's
resource-only MCP view to continue from saved progress and the saved next batch.
It returned the exact numeric result and checkpoint file URIs in whole JSON.

The model completed seven successful MCP resource calls: two catalog calls and
five reads covering the checkpoint overview, root/tree navigation, saved progress
and saved batch. Host verification passed the unchanged checkpoint verifier,
whole-JSON result, advertised URI chain, exact content digests, absent sender
workspace/registry/tree and stable, changed live inputs. The host-observed task
span was 79,383 ms.
The [evidence receipt](../assessment-evidence/2026-11-12/checkpoint-luna-continuation.json)
records the verified result.

The saved checkpoint contained the prior calculation state and original input
bytes; the receiver's live progress and CSV had been changed after capture. The
sender workspace, registry and source tree were removed before the model started.
The model made no writes. This demonstrates a same-machine, direct MCP read
continuation from an adopted immutable checkpoint; it does not qualify cross-host
or Linux behavior, protected saved-version policy, or process-wide filesystem
isolation. The generic execution runtime retained host capabilities. Earlier
OpenCode / Free Router failures remain unchanged and unqualified. The prior
progress was seeded by the host; this does not establish recovery from an
interrupted model-generated turn.

No model retry, provider credential access or native source change occurred.
The host captured and transferred the fixture before dispatch; the model made
no file save or checkpoint capture. The checkpoint and traces are retained under
the private fixture root for review.
