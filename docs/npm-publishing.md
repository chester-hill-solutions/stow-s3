# npmjs publishing preparation

This is preparation for five public packages: `@chester-hill-solutions/stow-s3`
and its `-darwin-arm64`, `-darwin-x64`, `-linux-arm64` and `-linux-x64` carriers.
Public installation remains unverified. Use a source checkout; the install-surface
declaration stays `published: false`. Historical GitHub Packages release receipts
describe that earlier registry and are not npmjs readiness evidence.

`scripts/release-policy.json` still selects `publishPackages: false`. PyPI and
live-provider paths remain disabled by that policy. This preparation does not
configure trust, publish or stage a package, or change workflow permissions.

## Future OIDC requirements

The future workflow uses public npmjs, explicit public access and GitHub-hosted
runners. Direct trusted publication needs Node 22.14.0+ and npm 11.5.1+.
Its identity is owner `chester-hill-solutions`, repository `stow-s3`, workflow
filename `release.yml`; no environment is configured. Public repository identity
must also match all five package manifests. No long-lived publish token is used.
See [npm trusted publishing](https://docs.npmjs.com/trusted-publishers/) and
[scoped public packages](https://docs.npmjs.com/creating-and-publishing-scoped-public-packages/).

`npm-publish-readiness.json` is a reviewed local assertion, not an account query.
Every package defaults to unconfirmed. Organization ownership and account 2FA
alone cannot satisfy it. Future activation requires confirmed public-package,
trusted-publisher and direct-publish permission records for every package.
An unvalidated trust must have a recorded creation time and complete its first
OIDC publish within two days. A verified successful OIDC publish removes that
initial expiry. The script never updates these assertions itself.

The visibility gate requires correctly named anonymous metadata for all five
packages. Absence, denied access, server failures, redirects, malformed responses
and transport failures stop publication. Exact-version preflight accepts only
HTTP 404 as absence and refuses identity/integrity conflicts. Repeated runs skip
only identical published bytes. The publisher verifies a private artifact copy,
uses empty npm configuration and confirms the published integrity afterwards.
Exact anonymous consumer installation remains a separate post-publication gate.

## Separately authorized bootstrap

1. Qualify the real reviewed release artifacts and the existing release gates.
   Establish package names, ownership, public access and publication authority.
2. With separate approval, the maintainer can first publish the real platform
   packages, then the core package, through interactive npm login and 2FA.
   This is actual publication, not account setup or a dry run.
3. Promptly configure each package's exact GitHub trusted publisher. Packages
   must exist first. The trust CLI requires npm 11.15.0+; current web settings and
   CLI permissions differ, so verify the actual direct-publish permission.
   Stage-only/default trust does not authorize this direct-publish workflow.
   See [npm trust](https://docs.npmjs.com/cli/v11/commands/npm-trust/).
4. Review the readiness records, first-publish expiry and existing live-provider
   requirements before separately authorizing workflow publication. Leave PyPI
   out of that approval unless independently requested.

An alternative staged bootstrap also needs separate approval. `npm stage publish`
can create the publicly visible `0.0.0-stage` placeholder for a new package; it is
not a no-side-effect dry run. Staged publishing needs npm 11.15.0+ and Node
22.14.0+, and final approval requires 2FA. See
[staged publishing](https://docs.npmjs.com/staged-publishing/).

Local `publish-if-absent.mjs --check` and `--dry-run` only plan registry reads;
they never invoke npm publish, stage or trust commands. Tests inject registry
responses and publication callbacks. No bootstrap action is part of those tests.

## First npm 0.3.5 bootstrap

Live-provider compatibility is **NOT verified for 0.3.5**. The owner waived the
three provider receipts only for this first interactive npm release. No verified
receipt is fabricated. Future automated publication still requires the existing
provider evidence, readiness assertions and enabled release policy. PyPI is off.

The separate `scripts/bootstrap-first-npm.mjs` accepts only all five Stow packages
at 0.3.5. It cannot publish from CI or another version. The default OIDC publisher
never imports this exception. No trusted publisher is configured by this script.

1. Review and merge the preparation through normal PR gates.
2. Rehearse version 0.3.5 on the exact merged main commit. Require successful
   push CI and the completed four-platform nonpublishing Release rehearsal.
   The rehearsal uploads `npm-first-0.3.5`: five real tarballs and a receipt
   recording source, run, canonical package identities and SHA-512 hashes.
3. Use a clean checkout of that current main commit. Invoke the bootstrap script
   with `--check`, the approved 40-character commit, rehearsal run ID and push CI
   run ID. It downloads that run's artifact and performs anonymous registry reads.
   It never publishes, stages, configures trust or accesses the login credential.
4. Obtain separate approval for actual public publication of these exact bytes.
   Only then replace `--check` with `--publish` in a local interactive terminal.
   Node 22.14+ and npm 11.5.1+ are required. The script verifies account `wra-sol`
   against npmjs and asks for the exact approved source commit. npm handles human
   2FA directly; never put an OTP or credential in arguments, logs or this repo.
   The previously authorized default local npm login is used without reading it.
5. The script verifies every identity, hash and immutable registry version before
   the first write. It publishes platform packages before core, with public access
   and canonical registry overrides. Each private copy is rehashed before npm
   receives it. Only HTTP 404 counts as absence; unavailable or denied reads stop.
6. A partial failure stops the run. Repeat only with the identical source, run and
   tarballs; identical registry bytes are skipped and conflicting versions fail.
   After publication, anonymous installs cover all five exact versions and the
   core consumer runs on the current native host. A failed verification is a
   partial release, not proof of success. Do not replace immutable version bytes.

This preparation does not execute step 4. Account ownership and enabled 2FA do
not prove package existence, organization publishing rights or OIDC trust.
Trust configuration and future automated publication require separate approval.
The install-surface declaration stays `published: false` until public installs
are actually verified. Staged placeholder publication is not used here.
