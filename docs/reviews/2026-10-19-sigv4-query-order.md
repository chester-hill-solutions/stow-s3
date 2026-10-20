# SigV4 query order follows the encoded names — 2026-10-19

This closes C07, the last required fix in the
[bounded closure register](2026-10-18-correctness-closure.md). The canonical
query string sorted the decoded parameter names and encoded them afterwards, so
a name that needs encoding could be ordered differently from the one a client
signed.

## The defect

The signing rules encode the query names and values first and sort them after.
`internal/auth` did the opposite: it parsed the raw query into decoded names,
sorted those, and encoded each one as it wrote it out.

For `?z=last&%C3%A9=first` the two orders differ. The decoded names are `z`
and `é`, and `z` sorts first. The encoded names are `z` and `%C3%A9`, and `%`
sorts before `z`. The canonical query is therefore `%C3%A9=first&z=last`, which
is what the pinned Node signer produces and what stow was not computing. A
request that signer had signed was refused, while an ASCII query — where the two
orders agree — authenticated.

## The correction

The names and values are encoded first and the encoded names are sorted, with
the encoded values sorted within a name. That is the order the rules use, and it
is the order the pinned Node signer produces.

The repository's own test signer encoded the same rule and changed with it, so
the tests that sign and verify against each other still agree.

## The evidence

`TestPinnedNodeSignerRequestsAuthenticate` reads the recorded output of
`@smithy/signature-v4` 5.7.4 with a fixed signing date and fixed credentials,
and requires both the ASCII request and the encoded-name request to
authenticate. The fixture is the same one the closure review recorded, byte for
byte, so the reference did not move.

`TestCanonicalQueryOrderFollowsTheEncodedNames` pins the canonical query
directly for an ASCII query, an encoded name, an encoded value and a repeated
name.

Against the previous implementation the encoded-name cases fail and the ASCII
control passes. Against the corrected implementation every case behaves as the
rules require. The existing SDK, malformed-query and repeated-query cases are
unchanged and still pass, and no request feature or signature fallback was
added.

## Scope

This is a bounded correction of one reproduced defect. It is a low-priority
compatibility defect for unusual query-name inputs and was never evidence that
an ordinary ASCII S3 request failed. No public API, dependency or storage
format changes.

With this fix the bounded closure register has no required fix left: seven of
its nine leads are closed by fresh regressions, one is optional input
strictness with no demonstrated security consequence, and the two that remain
open are the ones this and the previous corrections closed.
