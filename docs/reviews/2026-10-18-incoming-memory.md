# Incoming-write memory assessment — 2026-10-18

## Decision

Whole-object buffering is a demonstrated material cost for supported native inputs and raised HTTP request limits. It is **not evidence that the documented default request/admission contracts are broken**. Keep those limits and the existing publication/recovery behavior. The next implementation should target one measured storage copy or buffering path with paired before/after evidence; this change records the baseline and does not introduce an optimization.

At the default 8 MiB body size, unbound S3 UploadPart allocates **42,936,152 bytes**, versus **34,576,944 bytes** for unbound S3 PUT. At an explicitly raised 64 MiB HTTP cap, those totals are **641.9 MB** and **366.5 MB**. Source inspection identifies the additional part materialization; these different operations are not a substitute for a paired before/after patch benchmark. Sixteen admitted 8 MiB PUT requests retain about **156.8 MiB** at the instrumented handoff. This supports a focused body-ownership investigation before any broader streaming redesign.

The baseline is `9032c46321d7c12372be16d38aff149ee824f31e` (`9032c46`, streamed capacity scanning), with freshly fetched `origin/main` still `219d1dcfff9807b04660a40313b32127e5be2933`. The earlier COPY/precondition, protobuf-record, capacity-scan and reconciliation work is retained. Production source is unchanged. No dependency, storage format, public API, index, checkpoint, abstraction or process-memory limit is added.

## Existing promises and reachable paths

[Compatibility contract, Appendix C](../compat-contract.md#appendix-c--request-and-upstream-transport-boundaries-030) documents an **8 MiB** default per HTTP body and **16 active handlers**, configurable by positive native CLI flags. Excess body bytes receive `EntityTooLarge`; excess handlers receive `SlowDown` before body reads. The contract explicitly excludes process-memory isolation. Storage quotas are separate physical/logical admission contracts. Public/internal Go store readers do not inherit the HTTP body cap; caller/runtime limits remain separate.

The measured paths are the current filesystem implementation:

- `StageObject` selects buffered preparation when capacity-bound, guarded or request-keyed; otherwise bodies above its small prefix stage to disk. `storage.PreparePut` owns an incoming slice and uses size hints only for allocation.
- `commitBufferedLocked` transfers the prepared body into the existing buffered publication path. Capacity-bound `writeRecord` uses `encodeObjectRecord`, appending payload to the encoded prefix for admission and physical resource hashing. Guard comparison, request meaning, durable receipt and recovery checks remain required behavior.
- `UploadPart` calls `ETagForReader`, which materializes its input. `CompleteMultipartUpload` reads every selected part, then assembles a combined buffer. The public S3 part handler first owns its validated HTTP body and lends a reader to this store path.
- S3 PUT owns one body for auth preparation/integrity checks and transfers it via `OwnedBody`. Consequently fixing filesystem staging alone would not eliminate HTTP request materialization. This assessment does not label HTTP buffering itself a correctness failure.

Hypotheses were declared before measurements: native staging would scale with payload, bound record encoding would add another large allocation, and HTTP preparation might dominate native streaming gains. The measurements distinguish those predictions rather than attributing every allocation to storage.

## Method and limits

All project execution is on authorized Railway scratch deployment `6094ab3a-5240-413c-9ac3-8be9eaa9c43c`, service `83497842-c51d-420e-8ba1-181589730ad4`, environment `3cd200cb-c94f-4e4d-9335-0f6c1b7e9411`, project `8d90188e-0f1e-4c47-9379-9399ae06e6fa`. The existing target stays stopped and unchanged. Go 1.25.6 / Node 25.9.0, Linux amd64, UID 10001, GOMAXPROCS 4, runner 4 CPU / 4 GiB. Input and ledger fixtures use verified ext4 `/results` (statfs 61267).

One fresh Go test process per scenario, **41 scenarios × three rounds = 123 child processes**, alternating forward/reverse case order; **225 phase observations**. Sizes are 1, 8 and 64 MiB. Native input is an on-demand repeat-byte generator, not a preallocated client slice. `bound-hinted`/`guard-hinted` expose the existing `ReadSize` allocation hint; unhinted inputs deliberately omit it. The unbound `plain` profile is the existing disk-streaming control, not an alternative implementation of capacity/guard contracts. Profiles have different guarantees and are not interchangeable.

Each phase starts after GC; `TotalAlloc`/`Mallocs` deltas capture allocation, and monotonic elapsed time excludes the post-phase collection. Post-GC `HeapAlloc` delta measures retained heap while pending stages/blocked requests are reachable. Negative commit/abort deltas indicate previously retained data was released; they are not negative allocation. GC and collection overhead is excluded from the phase duration but contributes to the process footprint. These are instrumented single-operation measurements, not steady-state throughput or statistically significant latency estimates. Median/min/max are saved; no timing optimization is claimed.

Child RSS is Linux `ru_maxrss` for the entire isolated test process, including setup, all phases, validation and reopen. It is **not per-phase RSS**, not a cgroup/page-cache reading and not an OOM proof. The same child RSS appears on its multiple phases. HTTP cases invoke the real handler in-process with generated request bodies and valid SHA-256 body headers, explicit Content-Length and `DevBypass` auth. Auth preparation/body integrity remain in the handler; signing, credential-verifier CPU, sockets/TLS, SDK client memory, run-through/outbox and public runtime wrappers are not measured. 64 MiB HTTP cases explicitly raise the request cap to 64 MiB. Default-limit cases retain 8 MiB; no 64 MiB default acceptance is claimed.

The sixteen-request case deliberately pauses at `Store.PutObject`, after body ownership handoff. It measures a possible admitted resident set and then releases every request to complete. This deterministic barrier is test instrumentation, not a claim that all requests naturally align there in a production trace. Sixteen native stages likewise stay pending until all exist, then abort. Neither barrier changes production code.

## Native staged writes

| Profile / phase | Allocated, 1 MiB | Allocated, 8 MiB | Allocated, 64 MiB | 64 MiB retained heap delta | 64 MiB child peak RSS |
| --- | ---: | ---: | ---: | ---: | ---: |
| plain / stage | 0.197 MiB | 0.197 MiB | 0.197 MiB | -0.01 MiB | 12.72 MiB |
| plain / commit | 0.039 MiB | 0.039 MiB | 0.039 MiB | 0.02 MiB | 12.72 MiB |
| bound / stage | 5.001 MiB | 39.774 MiB | 375.243 MiB | 75.18 MiB | 195.09 MiB |
| bound / commit | 1.113 MiB | 8.113 MiB | 64.117 MiB | -75.15 MiB | 195.09 MiB |
| bound-hinted / stage | 1.010 MiB | 8.010 MiB | 262.713 MiB | 78.14 MiB | 197.46 MiB |
| bound-hinted / commit | 1.113 MiB | 8.114 MiB | 64.117 MiB | -78.12 MiB | 197.46 MiB |
| guard / stage | 5.001 MiB | 39.774 MiB | 375.243 MiB | 75.18 MiB | 159.23 MiB |
| guard / commit | 0.040 MiB | 0.040 MiB | 0.040 MiB | -75.17 MiB | 159.23 MiB |
| guard-hinted / stage | 1.010 MiB | 8.010 MiB | 262.713 MiB | 78.15 MiB | 197.33 MiB |
| guard-hinted / commit | 0.040 MiB | 0.040 MiB | 0.040 MiB | -78.14 MiB | 197.33 MiB |
| save / stage | 5.000 MiB | 39.774 MiB | 375.243 MiB | 75.18 MiB | 160.28 MiB |
| save / commit | 1.117 MiB | 8.120 MiB | 64.119 MiB | -75.15 MiB | 160.28 MiB |

Buffer retention in stage profiles and extra allocation at bound commit are separate costs. Size hints reduce growth allocation where effective, but do not make retained payload memory constant. Guarded absence and a fresh request-keyed durable save are measured; replacing an existing fingerprint-guarded object, save replay and unresolved recovery are not performance-qualified here. Those contracts must be covered before changing their implementation.

## Multipart ingestion and completion

| Profile / phase | Allocated, 1 MiB | Allocated, 8 MiB | Allocated, 64 MiB | 64 MiB retained heap delta | 64 MiB child peak RSS |
| --- | ---: | ---: | ---: | ---: | ---: |
| part-unbound / upload-part | 5.002 MiB | 39.775 MiB | 375.244 MiB | -0.01 MiB | 195.17 MiB |
| part-unbound / complete-part | 2.057 MiB | 16.058 MiB | 128.058 MiB | 0.03 MiB | 195.17 MiB |
| part-bound / upload-part | 5.075 MiB | 39.849 MiB | 375.318 MiB | -0.00 MiB | 205.84 MiB |
| part-bound / complete-part | 3.263 MiB | 24.269 MiB | 192.279 MiB | 0.04 MiB | 205.84 MiB |
| http-part-unbound / http-upload-part | 2.041 MiB | 40.947 MiB | 612.166 MiB | -0.01 MiB | 298.17 MiB |
| http-part-unbound / complete-part | 2.057 MiB | 16.058 MiB | 128.058 MiB | 0.02 MiB | 298.17 MiB |
| http-part-bound / http-upload-part | 2.098 MiB | 41.021 MiB | 612.245 MiB | 0.01 MiB | 326.00 MiB |
| http-part-bound / complete-part | 3.275 MiB | 24.271 MiB | 192.279 MiB | 0.04 MiB | 326.00 MiB |

The post-GC part-upload retained heap is small because the call releases its body; the high allocation/RSS is transient working memory, not evidence of a leak. Each case uses one valid final part, so there is no minimum-nonfinal-part exception or synthetic direct part-file setup. Uploads execute native APIs or the actual S3 UploadPart handler, then the native completion API. ETag, assembled SHA-256 content, metadata and settled capacity are checked. The completion is not the HTTP XML/completion transport. No multi-part-total peak bound follows from this one-part sample; source inspection shows completed assembly scales with selected part bytes.

## S3 PUT and concurrency

| Profile | Size | Phase | Allocated | Retained heap delta | Whole child peak RSS | Phase duration |
| --- | ---: | --- | ---: | ---: | ---: | ---: |
| http-bound | 8 MiB | http-put | 41.048 MiB | 0.02 MiB | 39.82 MiB | 68.082 ms |
| http-bound | 64 MiB | http-put | 413.564 MiB | 0.02 MiB | 209.96 MiB | 1329.546 ms |
| http-plain | 8 MiB | http-put | 32.975 MiB | 0.01 MiB | 29.57 MiB | 41.478 ms |
| http-plain | 64 MiB | http-put | 349.491 MiB | 0.01 MiB | 144.29 MiB | 1057.343 ms |
| http-sixteen-bound | 8 MiB | http-sixteen-at-store | 526.771 MiB | 156.78 MiB | 334.07 MiB | 26.767 ms |
| http-sixteen-plain | 8 MiB | http-sixteen-at-store | 526.772 MiB | 156.78 MiB | 292.50 MiB | 26.953 ms |
| stages-bound | 8 MiB | abort-sixteen | 0.000 MiB | -128.89 MiB | 306.44 MiB | 0.002 ms |
| stages-bound | 8 MiB | stage-sixteen | 636.397 MiB | 128.89 MiB | 306.44 MiB | 92.003 ms |
| stages-plain | 8 MiB | abort-sixteen | 0.002 MiB | -0.02 MiB | 14.41 MiB | 11.740 ms |
| stages-plain | 8 MiB | stage-sixteen | 3.172 MiB | 0.01 MiB | 14.41 MiB | 55.686 ms |

Single-request PUT data distinguishes HTTP body ownership from the streaming store control. Sixteen concurrent default-sized requests can retain their admitted body set even when the filesystem itself stages ordinary native readers to disk. The seventeenth handler is refused with 503 before its generator is read, so the admission promise still holds. This result is a sizing observation, not a new process-memory guarantee.

## Refusal controls

| Profile | Input | Phase | Allocated | Retained heap delta | Child peak RSS |
| --- | ---: | --- | ---: | ---: | ---: |
| http-refuse | 8,388,609 bytes | http-put | 32.935 MiB | -0.01 MiB | 29.65 MiB |
| refuse-bound | 8,388,608 bytes | refused-commit | 8.099 MiB | -8.03 MiB | 31.39 MiB |
| refuse-bound | 8,388,608 bytes | stage | 39.774 MiB | 8.04 MiB | 31.39 MiB |
| refuse-bound | 67,108,864 bytes | refused-commit | 64.100 MiB | -75.16 MiB | 205.16 MiB |
| refuse-bound | 67,108,864 bytes | stage | 375.243 MiB | 75.18 MiB | 205.16 MiB |

The native refusal profile binds a 4 MiB physical namespace (512 KiB recovery reserve). It consumes the staged 8/64 MiB body before commit refuses `capacity.ErrFull`; it publishes no object, leaves payload accounting unchanged and retains no pending reservation. Physical quota is not an incoming-body RAM admission mechanism. HTTP oversize is separately refused with `EntityTooLarge` at 8 MiB + one probe byte, with no published object.

## Preserved initial failure

The first job `2c10462c-070f-464c-a0a5-6a5bbc22039a` finishes all 12 steps, with only measurement marked failed. Its first 34 scenarios pass; scenario 35 reaches the sixteen-request body barrier and correctly rejects a seventeenth request, then the probe rejects valid returned metadata because it expected the native `source` map key instead of the S3 canonical `X-Amz-Meta-Source` key. No production behavior is changed. The original compiled probe, source capture, 35 command records, failure output and passing normal/race/conformance logs are retained under [failed-probe](2026-10-18-incoming-memory/raw/failed-probe/run.json). Only after that job is terminal and exported is the probe assertion corrected and the duplicate Content-Length header assignment removed. The final job reruns every case and every validation gate on the same deployment. Initial partial observations are excluded from final medians, never merged or discarded.

## Correctness, provenance and costs

All **12 Railway plan steps pass**: probe installation, formatting and exact source capture; Go binary build; all measured scenarios and their controls; removal of the temporary test; filesystem/capacity/S3 normal and race suites; memory/filesystem and runtime-memory/runtime-filesystem conformance. Each accepted native object/part lifecycle verifies size, ETag, complete SHA-256 content and metadata; native object/part cases close and reopen the store and recheck content and capacity. Request-keyed save checks its durable receipt. Uncommitted/aborted stages remain invisible. Native quota and HTTP body/concurrency refusals preserve the tested contracts.

[Raw measurements](2026-10-18-incoming-memory/raw/final/measurements.json), [ranges](2026-10-18-incoming-memory/raw/final/comparison-analysis.json), [checks](2026-10-18-incoming-memory/raw/final/checks.json), [receipt](2026-10-18-incoming-memory/raw/final/validation-receipt.json), [compiled probe](2026-10-18-incoming-memory/raw/final/compiled-probe.go.txt), [artifact manifest](2026-10-18-incoming-memory/raw/artifact-manifest.json). The source inventory and production hashes tie results to the baseline; the test binary hash and every child command/log remain saved. Probe source is retained as review evidence, not added to the production test suite. Additional report/site/documentation checks and final hash verification run on Railway before scratch teardown.

There is no runtime cost or caller migration from this measurement-only change. Evidence adds repository bytes and reproducing this qualification costs Railway CPU, disk and time. Current implementation retains payload-sized memory and full-file work in the measured buffered paths. Small samples, instrumented barriers and phase GC do not establish real-world throughput, p99 latency, container memory isolation or arbitrary-concurrency safety.

## Documentation capture limitation

The first report/site round passes site build and site tests, but the stock documentation checker fails `spawnSync git ENOBUFS`: the full repository path inventory exceeds Node execFileSync’s default 1 MiB stdout capture. The original logs and nonzero receipt remain in `raw/final/report-initial-*`. Validation is repeated on Railway with a [capture-only preload](2026-10-18-incoming-memory/raw/doc-links-capture.mjs) that raises stdout capture for that exact Git inventory command to 16 MiB. The original checker, path enumeration, link rules and checked documents are unchanged; no paths are filtered. This qualifies these document links with the larger harness capture. It does not claim the unmodified stock invocation passed. The checker needs a separate small tooling fix as the evidence inventory grows; no production script is edited in this measurement-only change.

## Next independently reviewable change

Prioritize the measured extra multipart ingestion copy, because it is reachable from the ordinary HTTP UploadPart path and can be investigated using the existing explicit body-ownership interfaces. First compare ownership transfer against this exact baseline; a full streaming part-write design is not the first step. Preserve part ETag/size, caller-buffer isolation, atomic replacement, physical admission, failure retention and completion/recovery behavior. Add a deterministic regression at the real native/S3 seam before changing production. Capacity-bound publication and multi-part completion need separate contract proofs; do not bundle them into that patch. No implementation of that next change is included here.
