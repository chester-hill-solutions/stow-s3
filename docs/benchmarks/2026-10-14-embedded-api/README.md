# Canonical embedded API and smaller WASM — 2026-10-14

The validated SDK WASM decreased from 6,504,060 to **4,610,006 bytes (29.121%)**. Its gzip and Brotli encodings decreased about 30.4%. The exact validated artifact was copied into the SDK distribution and its SHA-256 verified. All builds, tests, compression, and performance measurements ran on Railway.

## Change

`internal/embedded` now owns the common Go runtime facade, public store adaptation, streaming, staging, guarded saves, and object recovery operations. `pkg/stow` re-exports the canonical types and errors and retains the native filesystem/workspace constructors. Native adapters access the same runtime instance through internal-only bridge functions.

The WASM command imports the canonical embedded API directly. Its dependency closure excludes native workspaces, S3, run-through, AWS SDK packages, HTTP/TLS, native object records, and protobuf. Native workspace content sniffing continues to use the standard HTTP implementation. No sniffer was copied into production and no post-link optimizer or inlining change was adopted.

Bucket address validation uses `net/netip` instead of `net`; compatibility cases check the existing IP rejection rules. Registry and embedded object-recovery errors share a small platform-neutral sentinel vocabulary. The public Go symbol/method inventory is unchanged. Re-exported named types are aliases; reflection-qualified names of those types now identify the internal package.

## Size and exploratory performance

| Encoding | Before bytes | After bytes | Reduction |
| --- | ---: | ---: | ---: |
| Raw WASM | 6,504,060 | 4,610,006 | 29.121% |
| gzip, level 9 | 1,805,588 | 1,255,970 | 30.440% |
| Brotli, quality 11 | 1,328,674 | 924,486 | 30.420% |

Go 1.25.6 used `GOOS=js GOARCH=wasm`, `-buildvcs=false -trimpath -ldflags "-s -w"`. The pre-refactor archive reproduces the exact preceding SDK hash. Compressed sizes are artifact encodings, not deployed transfer measurements.

Six alternating fresh Node processes per variant measured the binary WASM bridge in the Railway build environment:

| Median observation | Before | After |
| --- | ---: | ---: |
| Read, compile, instantiate, and publish bridge | 67.181 ms | 51.182 ms |
| Above, then open bucket and first 4 KiB write | 92.687 ms | 74.179 ms |
| Verified 1 MiB PUT/GET pairs per second | 106.798 | 114.960 |

Each steady workload used five warmup pairs followed by 40 pairs, checking complete returned bytes. These exploratory medians exclude process spawn, `wasm_exec.js` evaluation, the JavaScript SDK facade, network sockets, browser startup, and host persistence. Shared build-host variation is not represented by a capacity guarantee. No website performance claim was changed.

Three matched legacy JSON/base64 working-set samples per version settled at 16 MiB of linear memory. Twenty subsequent read passes added no pages in either version. An earlier concurrent gate observed 256 KiB of first-pass scratch growth. That characterization assertion now checks stable repeated reads while retaining the first-pass growth measurement in the report. The [memory diagnosis](memory-diagnosis/verification/embedded-size/memory.json) and [final samples](final/verification/embedded-size/memory.json) preserve the observations.

## Validation and safeguards

The native-import regression failed on the original source and passes after extraction. The complete final Railway run passed repository standards, all direct/runtime memory/filesystem conformance lanes, Node/WASM tests, real browser tests, generated protobuf verification, and private chart rendering checks. Browser results: **33 passed, two optional cases skipped, zero unexpected failures or flaky cases**. Only the WASM changed in SDK generated output; the generated Go reference was synchronized.

Public contract tests continue to exercise the canonical implementation. Coverage now instruments module packages across test boundaries and merges repeated source blocks. A declared ownership mapping retains the original combined `pkg/stow` coverage floor; no floor was lowered. Coverage measured 74.47% with 11,448 covered statements against the existing 10,063-statement floor. Unit cases verify ownership folding and union of repeated profile blocks.

Superseded attempts exposed duplicate coverage accounting, an import placement mistake, constant-alias reference rendering, and the historical first-read assertion. These were corrected before the final complete run. The first export hit the archive buffer limit; a temporary recovery image retrieved the already retained artifact. The SDK copy was completed only after checksum verification.

## Evidence and retained identities

- [Full validation receipt](final/verification/receipt.json), [standards log](final/verification/standards.log), [browser report](final/verification/sdk-browser-report.json), and [export checksum](final/export.json).
- [Size comparison](final/verification/embedded-size/result.json), [startup/operation samples](final/verification/embedded-size/performance.json), and [generated-output change manifest](final/verification/generated-change.json).
- [Failing import regression](red.log), [original regression source manifest](red-source.json), and [validated Go source snapshot](validated-go-source.tar.gz).
- [Archived generated reference and rendered preview](final/verification/rendered-documentation.tar.gz). Raw before/after WASM binaries remain on the Railway results volume.

Final full-validation deployment: `f46a2abe-b6a3-493f-ac0c-bad635c88846` (`SUCCESS`). Memory diagnosis: `2f130a39-ac00-4ba5-870e-47e925456f8d` (`SUCCESS`, diagnostic only). Recovery deployment: `c60a4545-3a19-43e9-b35d-328f76677108`. Retention root: `/results/embedded-api-final-f46a2abe-b6a3-493f-ac0c-bad635c88846`.

SDK SHA-256: `299ee696ddc9147adae6aecf0dbe16572af76ace056383bbd2da5ba173dc71d8`. Both benchmark services were stopped after export. The [environment receipt](../railway-environment.json) records completion; charts remain unpublished.
