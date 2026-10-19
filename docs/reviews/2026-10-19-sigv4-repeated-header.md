# SigV4 repeated signed headers — 2026-10-19

This closes C06 from the
[bounded closure register](2026-10-18-correctness-closure.md). A signed header
contributed only its first value to the canonical request, so a request carrying
more values than it signed could still authenticate.

## The defect

`internal/auth` built the canonical request from `headerValue`, which returns
`values[0]` of a header and nothing else. The AWS SDK signs the whole set: each
value is trimmed, its inner whitespace collapsed, and the values are joined with
a comma.

Both halves of that were wrong in opposite directions.

A request signed by the pinned AWS Go signer over a header with two values
produced a valid signature that Stow refused, because Stow canonicalized the
first value alone. And a request signed over one value, with a second value
appended afterwards, still authenticated, because the appended value did not
change the canonical form Stow computed. The signature did not cover the value
that was added, and the verifier accepted it anyway.

## The correction

A signed header contributes every value the request carries. Each value is
normalized on its own — trimmed, with its inner whitespace collapsed — and the
values are joined with a comma, which is the form the SDK signs. The host header
keeps its own treatment, and the name is still matched case-insensitively.

Per-value normalization matters. Normalizing the joined string instead would
turn a value that itself contains a comma into two values, and the canonical
form would no longer be the one the SDK computed.

## The evidence

The differential uses the repository's own pinned `aws-sdk-go-v2` signer, so the
signature under test is the one a real client produces, and the public
`Verifier.AuthenticateAt` with fixed signing time and test credentials.

`TestARepeatedSignedHeaderAuthenticates` signs a request over one, two and three
values of a metadata header and requires each to authenticate.
`TestChangingASignedHeaderValueWithoutResigningIsRefused` signs over one value,
requires the request to authenticate, then adds, changes and removes a value in
turn and requires each to be refused.
`TestRepeatedSignedHeaderValuesAreNormalizedPerValue` signs over a padded value
and requires it to authenticate.

Against the previous implementation, the repeated and three-value cases are
refused and the appended-value case authenticates: the two halves of the defect.
Against the corrected implementation all of them behave as the contract requires.

The existing authentication and operation-header refusal tests are unchanged and
still pass, so name-case handling and the other refusals are untouched.

## Scope

This is a bounded correction of one reproduced defect. It changes how a signed
header is canonicalized and nothing else: no public API, dependency, storage
format or quality baseline changes. The
[S3 compatibility contract](../compat-contract.md) records the new rule, and the
closure register marks the lead closed.

The remaining required fixes in the register are C09, a case-insensitive
comparison of opaque ETags, and C07, the ordering of query names before URI
encoding.
