# Release readiness for 0.3.0

Superseded by the [2026-10-03 verification checkpoint](release-verification-2026-10-03.md)
for current gates and user actions. The account/visibility conclusions below are
historical: unauthenticated HTTP 401 does not establish private visibility on the
GitHub npm registry, which documents authentication for public packages too.

## The pipeline is proven

Rehearsal `36919082881` on `c998d42` — **14/14 jobs green**. It verified:

- the build, standards and generated-output gates
- all four platform builds and wheels
- the packed npm consumer on each of the four targets
- npm and wheel artifact contents, the publish preflight, and the checksum manifest

and published nothing, which is what a rehearsal is for. The workflow reports
`ACCOUNT_READY: false` and names the exact consequence: **a tag would stop at the
account gate before publishing anything.**

An earlier rehearsal on 30 Sep was also green, so the path was not newly proven —
but it was proven on a tree before W04 checkpoint capture and five dependency bumps,
so re-running it was the point. It caught a real defect: the site claimed a binary
size that was true only for the platform it was written on.

## Two things block a usable 0.3.0

### 1. The packages publish private, and the site says otherwise

Measured 2026-10-01, unauthenticated against the registry:

| Package | Unauthenticated `GET` |
|---|---|
| `@chester-hill-solutions/stow-s3` | **401** |
| `…-stow-s3-darwin-arm64` | **401** |
| `…-stow-s3-darwin-x64` | **401** |
| `…-stow-s3-linux-arm64` | **401** |
| `…-stow-s3-linux-x64` | **401** |

401 means present and private; 404 would mean absent. All five are 401, so all five
exist and none can be installed without org credentials.

`site/index.html` states `npm install @chester-hill-solutions/stow-s3` with no
caveat, and `check-site-claims` requires that exact string. So the gate enforces a
claim the registry contradicts, and it cannot see the contradiction: it reads the
page and the tree, never the registry.

**This is why the gate passes.** It is the same failure as the binary-size claim and
as this repository's first `check-site-claims` — a gate that cannot see the thing
under test is worse than no gate, because it is green.

I cannot fix this: `orgs/chester-hill-solutions/packages/…` returns 404 for me, so I
have no org admin. See [#28](https://github.com/chester-hill-solutions/stow/issues/28).

**Registry corrected 2026-10-03.** The measurements above are against
`npm.pkg.github.com`, which is where the private copies are and is now the chosen
registry — but they were originally read as though it were the registry the release
publishes to, and that was wrong. The committed `publishConfig` said
`registry.npmjs.org`, so the evidence was about a registry the pipeline did not use, and
a second conclusion drawn from it ("nothing has been published") was about npmjs rather
than about the packages. The 401s are real; the inference drawn from them was not.

`docs/agent-dx-plan.md` had already recorded this trap on 2026-09-25: a local `.npmrc`
bound the scope to GitHub Packages, so `npm view` reported `0.2.0` for a package absent
from npmjs, and that false positive happened "during this very work". Worth re-reading
before asking whether something is published, by any means.

The manifests, `publish-if-absent.mjs` and the release workflow have been retargeted to
`npm.pkg.github.com`, and the install instructions now carry the scope binding that
GitHub Packages requires. `access: public` is gone: GitHub takes visibility from a
package setting, so asserting it in a manifest asserted something the registry never reads.

**This is now blocked automatically.** `scripts/check-publish-visibility.mjs` requests
each package with no credentials and refuses the release if the registry demands them.
It runs *before* the publish steps, because visibility belongs to a package and not to a
release: publishing 0.3.0 would not change the answer, and checking afterwards could only
fail a job that had already published. Today it reports all five as private and exits 1,
so a tag cut before the packages are made public stops there rather than at the publish
step.

A rehearsal does not run it, because a rehearsal runs on a branch and this only has
anything to say about a tag.

The three ways out, and why the first:

| | what it takes | consequence |
|---|---|---|
| **Make the packages public** | org owner, ~2 min | the page's claim becomes true; no code change; reversible in the UI |
| Document an authenticated install | repo change only | honest, but adoption needs an org invite, which is poor for a product whose point is deployable ACLs |
| Publish to public npm | npm org + automation token | stronger distribution; a public scope is semi-permanent, so it is a separate decision |

### 2. `vars.STOW_NPM_PUBLISH_READY` is unset

The repository has no variables at all. The workflow already handles this correctly:
it warns and continues on a rehearsal, and stops on a tag. So this is the account
setup, not a defect.

## A release on GitHub Packages needs two registries, not one

Retargeting exposed this and the rehearsal caught it: setting `--registry` to GitHub
Packages sent **every** dependency lookup there, including third-party ones, and GitHub
Packages does not mirror npmjs. All four packed-consumer jobs failed with

```
npm error 404 Not Found - GET https://npm.pkg.github.com/@aws-sdk%2fclient-s3
  - npm package "client-s3" does not exist under owner "aws-sdk"
```

`--registry` sets the default for all package names, so it cannot express "our packages
come from here and everything else comes from npmjs". The working configuration is both:

```
--registry=https://registry.npmjs.org --@chester-hill-solutions:registry=https://npm.pkg.github.com
```

`publishConfig.registry` still names GitHub Packages, because that is where `npm publish`
sends the tarball. `setup-node`'s `registry-url` also names GitHub Packages, for the
credentials it writes. Only the *install* steps need the default left on npmjs.

So a consumer of this package needs the same two-registry configuration, which is the
cost the plan's exit criterion could not survive, and why the install instructions carry
the scope binding rather than a registry change.

## What is verified about the product itself

Audited against the tree on 2026-10-01, because one claim on the front page had been
false and one false claim is evidence that others might be:

- **Four language surfaces** — true. Go (`pkg/stow`, 87 files), TypeScript
  (`packages/stow-s3/dist` with declarations), WebAssembly
  (`dist/stow-runtime.wasm`, 6,111,013 bytes = 5.83 MB, matching the 5.8 MB claim),
  Python (`packages/stow-s3-py` with a `pyproject.toml` and CI tests).
- **Three storage modes** — true. `internal/runtime/types.go` defines exactly three
  `Backend` values: `memory`, `filesystem`, `workspace`. The plan's "native, Railway
  and Workers" are deployment profiles, a different axis, not a fourth mode.
- **Zero cloud services, no credentials to rotate** — consistent with the tree;
  ephemeral per-session keys are a documented property.

No further false claim was found. The registry-visibility claim is the one that is
false, and it is the one no gate can see.

## Held deliberately

- **#32, TypeScript 5.9.3 → 7.0.2.** Held through the release. It is a major of the
  compiler for a package that is about to be published, so a typecheck regression
  lands in every consumer's build. Cheap to hold; expensive to get wrong.
- **`authority.EnforcedElsewhere`.** Landed and machine-checked, but it is an escape
  hatch added to an enforcement gate, and six PRs merged with `--admin` rather than a
  review. It is the one design decision here I would want a second opinion on: if my
  reasoning were wrong, the gate would be green while wrong.
- **The comment budget sits at its floor of 4161 with zero headroom.** The ratchet is
  working as designed, but the consequence is that the next feature must first delete
  documentation from unrelated shipped code to earn its own comments. That is a bad
  trade recurring on every slice, and it deserves a decision rather than another
  collision.

## Order

1. Make the packages public (org owner).
2. `STOW_NPM_PUBLISH_READY=true`.
3. Tag `v0.3.0`.

Rehearsing again is unnecessary until step 1: visibility is an org setting the
pipeline cannot observe, so no run of this workflow will ever detect it.
