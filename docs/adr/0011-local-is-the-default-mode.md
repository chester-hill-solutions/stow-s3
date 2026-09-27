---
status: accepted
supersedes: 0001
date: 2026-09-27
---

# Local-only is the default; ambient credentials do not select an upstream

**Supersedes ADR 0001** in full. ADR 0001 is retained unchanged so the reasoning it
was accepted on stays auditable; this record is the amendment the 0.2.0 plan
required rather than an edit to accepted history.

## The invariant

One line: **no explicit request, no upstream.** Run-through mode is selected only
by `--mode run-through` or `STOW_MODE=run-through`. Credentials decide how a
requested upstream is *authenticated*; they never decide whether one is used.
`STOW_MODE=local` still forces local-only.

The hyphen in `run-through` is optional, because losing it is a typo of the same
word and the intent is unambiguous. Nothing else is accepted. `STOW_MODE=upstream`
is local like any other unrecognized value — it is a different word rather than a
misspelling, and it was the name of the reserved-and-unused start option the
0.2.0 plan removed, so honouring it would have let the configuration that plan set
out to delete select a live provider.

The `--mode` flag default moved from `auto` to empty. An `auto` default
advertises in `--help` that something else can select run-through, and the thing
that used to select it was the machine's AWS configuration.

## Why ADR 0001's mitigation did not cover it

ADR 0001 argued for auto-detect partly on the grounds that the loud startup banner
and read-only default writes mitigated a `.env` copied from staging. Both of those
cover **writing**. Neither does anything about **reading**, which is the half that
is easy to trigger by accident: a single read against a real bucket is a real read
of real data, and no banner prevents it from being issued.

ADR 0001's own premise is the argument against it now. Its reason for preferring
auto-detect was that "developers already have live vars in their environment". That
is true, and it is exactly the problem: a CI runner exports `AWS_*` for unrelated
tools, so whether stow contacted a live provider was a property of the machine
rather than of anything the user asked for.

## What this does not change

The separate live-write consent requirement in ADR 0005 is untouched and is not
weakened by this record. A run-through configuration carrying a `mirrorWrites`
policy is still not permission to mutate upstream: it writes locally, reports
`mirrorWrites-disabled`, and prints a banner that does not warn about propagation.
The two decisions have the same shape — a configuration that expresses intent is
not the intent — which is why they now have the same test.

## Consequences

- The local-mode startup banner names how to ask for run-through, because "the
  configuration was not asking" and "the credentials were absent" are
  indistinguishable from outside.
- An unrecognized `STOW_MODE` is local rather than an error or a fallback.
  Refusing to start would mean a typo stops a dev server that is otherwise
  harmless; falling back to detection would mean the typo re-enabled exactly what
  this record removes.
- `docs/compat-contract.md` states the mode invariant and the consent requirement
  in the two places that previously described detection and the write policy.
- `CONTEXT.md` gains a Mode Selection entry and a separate Live-Write Consent
  entry, because a policy that reads as a consent is the confusion this decision
  exists to remove.
