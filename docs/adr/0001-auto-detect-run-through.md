---
status: superseded
superseded_by: local-is-the-default-mode
---

# Auto-detect run-through mode from environment variables

> **Superseded.** The behaviour this record describes no longer exists. Stow runs
> local-only unless something asks for run-through by name; ambient credentials no
> longer select an upstream. The reasoning below is kept because it explains what
> the change had to weigh against, and the "Considered Options" section reads very
> differently now that the trade-off has been decided the other way.
>
> See [ADR 0005](0005-live-write-requires-explicit-consent.md) for the
> neighbouring decision, which this one used to sit on top of.

## Original decision

When `Stow.start()` is called without an explicit mode, stow inspects environment variables for upstream S3-compatible credentials (`STOW_*` > `S3_*` > `AWS_*`). If endpoint, access key, and secret key are present, stow starts in run-through mode with the `readThroughCache` policy. Otherwise it starts local-only. Writes always stay on the local object store unless `allowLiveWrites: true` is set explicitly. Developers can force local-only behavior with `STOW_MODE=local`.

We chose auto-detect over local-default because CHS applications already configure S3 via standard AWS/S3 environment variables. Requiring developers to rename vars or pass explicit upstream config duplicates configuration they already have in `.env` files. The trade-off is surprising behavior when a `.env` copied from staging silently enables upstream reads — mitigated by a loud startup banner, read-only default writes, ETag-based cache revalidation, and the `STOW_MODE=local` override.

## What turned out to be wrong

The mitigation assumed the environment was the developer's own configuration. It is not: `AWS_ACCESS_KEY_ID` and its companions are exported by CI runners, by shells set up for the AWS CLI, and by every other tool that talks to S3. So "credentials are present" was true on machines where nobody intended stow to contact anything, and the `.env` hazard was only one of the ways in.

The banner and the write-consent decision both mitigate *writing*. They do nothing about reading, which is the half that was easy to trigger by accident, and a wrong guess there is a real read of a real bucket.

Naming the mode also turned out to be cheap. The configuration the auto-detect path existed to avoid writing is one variable — `STOW_MODE=run-through` — next to the credentials it already reads.

## Considered Options

- **Local-default:** Chosen instead. "stow is always local unless I opt in" is the model a development tool should have, and the cost is one environment variable for the users who want otherwise. This was rejected originally on the grounds that developers already have live vars in their environment; that is true, and it is the problem rather than the justification.
- **Full proxy:** Forwards all traffic to upstream for exact live behavior; rejected as default because it hammers staging/production on every read and enables accidental mutations without explicit opt-in.
- **Single-bucket run-through:** Only proxy the bucket named in `S3_BUCKET`; rejected because multi-bucket apps are common and bucket names in SDK requests should map transparently to upstream.

## Consequences

- Startup must print mode, upstream endpoint (credentials redacted), write policy, and override hints on every start.
- In local mode the banner must name how to ask for run-through, because "the configuration was not asking" and "the credentials were absent" are indistinguishable from outside.
- CONTEXT glossary defines Mode Selection, Read-Through Cache, and Upstream Configuration distinctly from Local-Only Mode.
- Conformance tests must cover both local-only and run-through paths against AWS S3, Cloudflare R2, and custom endpoints.
