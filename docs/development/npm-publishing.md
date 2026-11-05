# npmjs publishing

The five public Stow packages are published at 0.3.5:
`@chester-hill-solutions/stow-s3` and its `-darwin-arm64`, `-darwin-x64`,
`-linux-arm64` and `-linux-x64` carriers. Public installation is verified.
Historical GitHub Packages receipts describe a separate registry.

`scripts/release-policy.json` still selects `publishPackages: false`.
This release used the separately authorized interactive bootstrap.
Future automated publication, PyPI publication and trusted publishing remain disabled.

## Published 0.3.5

The November 5, 2026 publication used the exact five tarballs from successful
[Release rehearsal 42062425692](https://github.com/chester-hill-solutions/stow-s3/actions/runs/42062425692).
The receipt identifies source `47733b6a9d3c74c3aac1b312519523b962ff7d93` and each SHA-512 integrity.
[Push CI 42062181393](https://github.com/chester-hill-solutions/stow-s3/actions/runs/42062181393)
passed for that same source. The published registry integrity matches every artifact.

Each exact version installed anonymously using empty npm configuration and a fresh cache.
The macOS ARM64 consumer selected its installed carrier, started Stow 0.3.5,
and prepared and served a persistent workspace. The rehearsal separately passed
packed consumer checks on all four native platforms.

npm accepted uploads before their versions became publicly readable.
The bootstrap stopped at its immediate integrity check during this processing delay.
Each continuation waited for matching public integrity, then skipped completed versions.
No published version was replaced. Platform packages became available before the core package.

After publication, the four source lock records were resolved against public npmjs with matching SHA-512 checksums.
The earlier empty optional-package placeholders fail clean installation once these packages are public.
The version gate now requires each carrier's declared version, and current `main` contains the complete records.

Live-provider compatibility remains **NOT verified for 0.3.5**.
The existing owner waiver applied only to this first interactive npm release.
No trusted publisher was configured, and no package was published to PyPI.

The immutable 0.3.5 tarballs retain their prepublication README text.
GitHub and the live documentation now contain the verified installation instructions.
npm refreshes its package-page README only with a new package version; see
[npm README guidance](https://docs.npmjs.com/about-package-readme-files/).

## Future OIDC requirements

The future workflow uses public npmjs, explicit public access and GitHub-hosted
runners. Direct trusted publication needs Node 22.14.0+ and npm 11.5.1+.
Its identity is owner `chester-hill-solutions`, repository `stow-s3`, workflow
filename `release.yml`; no environment is configured. Public repository identity
must also match all five package manifests. No long-lived publish token is used.
See [npm trusted publishing](https://docs.npmjs.com/trusted-publishers/) and
[scoped public packages](https://docs.npmjs.com/creating-and-publishing-scoped-public-packages/).

`npm-publish-readiness.json` is a reviewed local assertion, not an account query.
Trusted-publisher and direct-publish permissions remain unconfirmed for every package.
Organization ownership and account 2FA alone cannot satisfy them.
Future activation requires confirmed public-package,
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
   After publication, anonymous installs use fresh disposable caches for all five
   exact versions. The core consumer verifies its actual installed host carrier
   before starting; PATH and environment fallbacks cannot pass. A failed verification is a
   partial release, not proof of success. Do not replace immutable version bytes.

The first 0.3.5 publication completed this procedure; see the publication record above.
Account ownership and enabled 2FA alone do not prove package existence or OIDC trust.
Trust configuration and future automated publication require separate approval.
The install-surface declaration now records the verified public npm installation.
No staged placeholder was used.
