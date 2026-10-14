# Further WASM size opportunities — Railway exploration

The largest measured opportunity is removing native HTTP dependencies from the embedded WASM import closure. An attribution prototype reduced raw size from 6,504,060 to 4,652,200 bytes (28.472%). Brotli size fell 29.886%. This is an experiment, not a validated release patch. Production Go and the shipped SDK binary remain unchanged.

## Measurements

All builds, smoke tests, compression, and numerical analysis ran on Railway. Each experiment used Go 1.25.6 and the existing release flags: `GOOS=js GOARCH=wasm`, `-buildvcs=false -trimpath -ldflags "-s -w"`. Both independent batches reproduced the exact validated SDK SHA-256: `d41c489a93c7a349de69c5bb415b04f4de3964bdacb12fdde8557d9d6eed9283`.

| Experiment | Raw bytes | gzip level 9 bytes | Brotli quality 11 bytes | Raw reduction from baseline |
| --- | ---: | ---: | ---: | ---: |
| Current validated SDK | 6,504,060 | 1,805,588 | 1,328,674 | — |
| Lighter IP parser alone | 6,501,947 | 1,805,109 | 1,328,067 | 0.032% |
| Exclude native workspace facade imports | 5,909,904 | 1,657,067 | 1,229,207 | 9.135% |
| Facade exclusion plus lighter IP parser | 5,907,813 | 1,656,405 | 1,229,800 | 9.167% |
| Above, with compiler inlining disabled | 6,487,433 | 1,664,409 | 1,210,181 | 0.256% |
| Remove remaining native HTTP import closure | 4,652,200 | 1,264,837 | 931,587 | 28.472% |
| Above, then Binaryen `-Oz` | 4,379,510 | 1,260,753 | 939,096 | 32.665% |
| Current SDK, then Binaryen `-Oz` | 6,116,969 | 1,798,184 | 1,341,942 | 5.952% |

Sizes are deterministic artifact measurements. Compressed values do not measure deployed transfer, compile time, startup, or throughput. Disabling inlining increases raw size compared with the same import isolation using normal flags. Binaryen reduces raw size, but increases Brotli size in both the baseline and network-free comparisons. It is a lower-priority option for distribution over Brotli.

The stripped baseline retains only 185 bytes of custom-section payload (`go:buildid` and `producers`). Further metadata stripping offers negligible savings. Its code section contains 4,094,426 bytes and data section 2,397,181 bytes.

## Attribution and recommended code work

`pkg/stow/workspace_facade.go` imports the native S3 facade. That transitively initializes the server, run-through adapter, AWS SDK, and HTTP dependencies when the embedded program imports the whole public package. The WASM command does not expose the native workspace facade.

Three native workspace files also import `net/http` solely for `DetectContentType`: `objects.go`, `json.go`, and `rooted_reads.go`. Removing the facade alone therefore leaves HTTP/TLS in the dependency closure. The storage bucket validator's `net.ParseIP` adds another network dependency; `net/netip.ParseAddr` removes that remaining import in the complete prototype.

The facade experiment substitutes an unsupported-platform attribution stub in the cloud source. The network-free experiment additionally calls the exact Go 1.25.6 content sniffer from a temporary local package. It preserves that sniffer's algorithm while measuring the cost of HTTP package initialization. The copied source and Go license are retained in the evidence. This is not a proposal to vendor the sniffer or weaken native content detection.

The production refactor should separate the embedded runtime/API from native workspace implementations and keep the native facade and content sniffing fully functional. Shared API types and adapters should remain canonical. Add a WASM import-boundary regression excluding the native HTTP/server/AWS closure. Then run the full standards, native conformance, SDK, and browser gates, synchronize generated artifacts, and measure startup and operation performance before adoption.

The complete prototype reduces the dependency count from 269 to 123. The [checked dependency analysis](network-probe/analysis.json) identifies every removed package, including `net`, `net/http`, `crypto/tls`, `crypto/x509`, the AWS SDK, and the run-through/S3 server. Required object integrity hashes and secure randomness remain linked.

## Validation limits and evidence

Every built variant passed WebAssembly binary validation, the existing real-memory runtime integration test, and the edge-shaped isolate smoke probe. These checks do not establish full SDK/browser parity, host-backed persistence, or performance neutrality. No production bundle was replaced, native storage behavior changed, or website chart published.

- [First experiment results](probe/result.json), [exact first executed script](probe/executed-probe.mjs), and [first export checksum](first-export.json).
- [Network experiment results](network-probe/result.json), [checked analysis](network-probe/analysis.json), [exact executed script](network-probe/executed-probe.mjs), and [uploaded Dockerfile](network-probe/uploaded.Dockerfile).
- [Network export checksum](network-export.json), [first source manifest](first-source.json), and [network source manifest](network-source.json).
- Smoke-test logs and full dependency lists accompany each variant in the two evidence directories. Text exports were checksum verified. WASM binaries remain only on the Railway results volume.

The first deployment `94282bb7-0158-403d-a697-29b679c000a2` and network deployment `c268be8c-2ac3-4f33-8b1c-a392f5bc5bc9` both reached `SUCCESS`. Retained artifacts are under `/results/wasm-size-probe-<deployment-id>/`. The runner's latest deployment and unchanged benchmark target were confirmed `REMOVED` after export.

Binaryen version 108 ran `-Oz --enable-bulk-memory --enable-sign-ext`; no assumptions about traps or closed-world callbacks were enabled. The [Binaryen optimizer documentation](https://github.com/WebAssembly/binaryen/wiki/Optimizer-Cookbook) describes the available passes. Go's [compiler documentation](https://pkg.go.dev/cmd/compile) defines `-l` as disabling inlining.
