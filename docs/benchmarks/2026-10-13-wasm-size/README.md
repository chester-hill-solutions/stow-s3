# WASM size regression — October 13, 2026

The SDK WASM is back to 6.14 MiB after reaching 9.35 MiB. The same-source Railway
comparison removes 3,365,444 bytes (34.3%). It is 2,859 bytes above the earlier
6,432,495-byte artifact, recovering almost all of the recent size growth.

| Artifact encoding | Before bytes | After bytes |
|---|---:|---:|
| Raw WASM | 9,800,798 | 6,435,354 |
| gzip, level 9 | 2,499,159 | 1,790,549 |
| Brotli, quality 11 | 1,793,250 | 1,317,851 |

Builds and compression ran on Railway with Go 1.25.6, `GOOS=js GOARCH=wasm`,
`-buildvcs=false -trimpath -ldflags "-s -w"`. Compressed figures describe these
encodings; they do not measure deployed network transfer or browser startup.

## Cause and correction

The shared Go API imported the native filesystem package on every platform.
Although native filesystem opening already refused unsupported platforms, its
package initialization still brought the protobuf implementation into WASM.

The filesystem implementation now compiles on Linux/macOS. Other platforms keep
the public function and return the same unsupported-backend error through a
small stub. Common option types remain available everywhere. The WASM dependency
closure excludes both the native object-record package and protobuf. Native
protobuf storage and its durability behavior remain intact.

The [size result](size/result.json), [before dependencies](size/before-dependencies.txt)
and [after dependencies](size/after-dependencies.txt) record the comparison.
The before artifact reproduces the exact SHA of the previously tested SDK:
`f4da0cecaa4d19307b77314be57a5b3cf50d9b784c175baead6b0e0a6a4fc2b0`.
The after artifact is:
`4493871fa6ccabba1a9950e233c3c2017f2f4cb073a5027f3cf5916176aeb9e0`.
The checked SDK artifact was copied back to source after validation.

## Validation

All builds, tests and analysis ran on Railway. The new dependency-boundary
regression [fails on the original source](size/regression-red.txt) and passes in
the fixed source's race/coverage suite. Windows cross-compilation verifies the
unsupported-platform API. The [full receipt](receipt.json) and [log](standards.log)
record repository standards, all four conformance lanes, Node/WASM tests, SDK
browser tests and protobuf generated-source verification. Browser tests passed
33 cases, with the optional 1 GiB and direct-provider qualification cases skipped.
The [chart receipt](preview-receipt.json) records 13 S3 chart checks, six screenshots
and zero browser errors. Only the SDK WASM changed in the generated-output
[manifest](generated-change.json).

Deployment `33e20f9f-c5e1-45cb-80a0-da89acce5a48` reached `SUCCESS`. The runner and
target were both read back as `REMOVED` after export. No website publication or
new throughput/cold-start measurements were performed. Historical benchmark
hashes continue to identify the artifacts actually measured in those runs.

The first build hit the existing comment-volume ratchet. Comments were tightened
without raising any baseline; an intermediate superseded build was cancelled.
The completion manifest retains these excluded deployment IDs.

The experiment can be packaged with `--full --size`. The verifier's baseline
SHA reference now reads the historical batch receipt rather than the replaceable
SDK copy; both references had the same hash in the validated run. Production
source matches the tested snapshot; subsequent edits index evidence and update
this equivalent baseline reference.

## Next performance work

Prioritize a fresh S3 GET profile and per-stage quarantine timings on Railway.
The existing filesystem PUT CPU profile spends 56.77% of sampled CPU in MD5;
that is a write-path lead, not an explanation of the S3 GET gap. Preserve quota,
content integrity and durable acknowledgement while testing targeted changes.
The smaller WASM needs a new cold-start measurement before any startup-speed
claim changes.
