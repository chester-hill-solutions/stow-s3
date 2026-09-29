# Compatibility acceptance coverage — 2026-09-29

This is the S0-4 traceability map for the finite supported contract. The shared
corpus has **51 cases** in `conformance/corpus/cases.json`. Both the Go AWS SDK
adapter and the Node AWS SDK/Smithy adapter execute the same cases. Go runs memory,
filesystem, and both runtime adapters; Node runs memory and filesystem. This is
local contract evidence, not a claim of complete AWS S3 equivalence or a fresh
live-provider compatibility result.

| Acceptance area | Shared cases / focused coverage | Boundary |
| --- | --- | --- |
| Presigned GET, PUT, HEAD | `presigned-get`, `presigned-put`, `presigned-head`; real Go v4 and Node Smithy signatures, HTTP execution; PUT checked through subsequent SDK GET | Full Go S3 presigner additionally covered by `conformance/TestPresignedGetPut` |
| Expired and future signatures | `presigned-expired`, `presigned-future`; error codes asserted | Malformed/zero/negative/fractional/oversized expiry and the seven-day upper boundary live in `internal/auth/TestPresignedExpiryValuesAreRefusedWhenUnusable`; duplicate and ambiguous auth queries in `TestPresignedAuthenticationParametersAreUnambiguous`. Raw verifier requests exercise shapes normal SDKs refuse to generate |
| Virtual-host routing | `presigned-virtual-host` uses a signed bucket hostname and a direct loopback transport; same server as the path-style cases | DNS/certificate provisioning is outside the local server contract; upstream SDK virtual/path-style signing separately covered by `internal/runthrough/addressing_wire_test.go` |
| Bucket and batch operations | `bucket-lifecycle`, nonempty deletion, batch existing/missing/quiet cases | Versioned deletion is unsupported |
| Multipart variants | Successful ordered completion and initiation metadata; `multipart-reject-{empty,duplicate,unsorted,wrong-etag,short-nonfinal,aborted,repeated}` | CRC64NVME full-object multipart declaration is explicitly refused and has a focused wire test; no CRC64 composite claim |
| Listing tokens | Existing ordered, delimiter, encoded and continuation-page cases; `list-token-reject-overlong`, `list-token-reject-nul` | Current tokens are lexical key markers: nonempty values must be valid UTF-8, at most 1024 bytes and contain no NUL. They are not signed, query-bound AWS tokens. Clients should only replay returned tokens |
| Conditional copy combinations | Matching If-Match, matching/stale If-None-Match, and combined If-Match/If-None-Match cases | Concurrent source replacement and coherent copied-version conditions remain in `conformance/copy_test.go` and `internal/storage/copy_contract_test.go`, which directly control mutation timing |
| Metadata | Mixed-case bare SDK input round-trip, copy/replace, multipart metadata; real upstream SDK/cache/offline tests in `internal/s3api/upstream_metadata_sdk_test.go` | Legacy full HTTP-prefixed stored maps are normalized at adapter boundaries; conflicting aliases fail |
| Checksums | CRC32, CRC32C, CRC64NVME, SHA1, SHA256, MD5 and bad-digest cases | `STOW_TEST_AWS_CLI=1` additionally tests the installed AWS CLI's default CRC64NVME request. Partial bodies do not advertise full-object checksums |

## Commands

- `make test-conformance` runs the Go matrices.
- In `packages/stow-s3`, run `tsx --test --test-name-pattern 'shared conformance corpus' test/integration.test.ts` against a freshly built local binary.
- `go test ./internal/auth ./internal/s3api ./internal/runthrough ./internal/storage/...` runs focused protocol, concurrency and backend contracts.

Malformed-signature and provider-specific token encodings are intentionally not
invented as SDK corpus fixtures. The table names the actual evidence and the
remaining product boundary instead of treating all S3 behavior as covered.
