# Workspace editing model pilot

**Date:** 2026-11-12. **Status:** the bounded live MCP-only model gate failed.
The local native and E.1/E.2 SDK binding remain qualified for their stated
macOS ARM64 `host_quiesced` profile. This single model attempt does not qualify
model editing, continuation, Linux, another physical host or release behavior.

## Attempt and evidence

The authorized attempt used OpenCode `v2.2.7` and the OpenRouter Free Router
selector. It ran one stage-one prompt in 7,494 ms of the 300,000 ms deadline.
The guard observed one successful `openrouter/free` request, resolved by the
provider to `liquid/lfm-3-1.7b:free`. The proxy enforced the strict response schema
and the complete four-tool catalog. The model returned terminal values with
zero MCP tool calls, so the trace verifier rejected the result. The response's
claimed committed outcome was unsupported: no save tool call or host receipt
existed.

Stage two did not start. No checkpoint or handoff was created. Host verification
independently confirmed that `docs/STATE.json`, `docs/batch-a.csv` and
`docs/batch-b.csv` remained byte-identical to their pre-prompt hashes; the sender
workspace and registry remain available. The fixture and failed trace are
retained at the paths recorded in the [receipt](../assessment-evidence/2026-11-12/workspace-editing-model-local.json).
The prior failed W25 receipts are unchanged. No retry, prompt salvage, UI change
or native-code change followed this result.

The provider's strict JSON schema constrained response shape but did not require
the model to execute the offered MCP tools. The failure therefore leaves the
central model-editing acceptance condition open. The verified SDK binding and
no-provider fixture demonstrate tool behavior and host verification, not that a
model uses them.

The corrected no-provider mock run passed both prompts, eight fake chat requests,
source removal before stage two and exact final checkpoint bytes. The final agent
suite passed 46 tests with one provider-opt-in test skipped. Those checks validate
the mocked caller flow and local safeguards only; they do not change the failed
live result. One earlier mock run that reached its 60-second timeout is retained
separately in the receipt.

## Next gate

The smallest next design step is to require a successful initial MCP read before
accepting terminal structured output, then request a separately authorized live
attempt. Keep the strict trace, saved-byte, receipt and checkpoint gates. Do not
infer a write from terminal claims or weaken acceptance to count the schema-only
response as a pass.

This record covers one local Free Router attempt only. The [E.1/E.2 record](2026-11-12-workspace-editing-mcp.md)
retains native and SDK qualification boundaries; the [W26 plan](../design/workspace-resource-plan.md#w26-f1-result)
and [canonical status row](../development/storage-foundation-plan.md) retain the
open model gate.
