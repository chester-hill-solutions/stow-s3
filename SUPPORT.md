# Support

## Where things go

| You want to | Use | Why |
|---|---|---|
| Report a bug | [Issues](https://github.com/chester-hill-solutions/stow-s3/issues) | Thirty issues exist and twenty-eight are closed, so the tracker is where the history is |
| Ask how to do something | [Discussions](https://github.com/chester-hill-solutions/stow-s3/discussions) | A question that belongs in an issue becomes noise in the bug list |
| Propose a change | An issue, before a patch | See [CONTRIBUTING.md](CONTRIBUTING.md) |
| Report a vulnerability | The private channel in [SECURITY.md](SECURITY.md) | Not a public issue |
| Check whether something is supported | The table below, and `stow-s3 doctor --json` | Faster than asking, and the diagnostic is the authority |

## Before you file a bug

Run these three and paste the output. They answer most questions, and a report
carrying them is usually fixed without a round trip:

```bash
stow-s3 --version
stow-s3 doctor --json
stow-s3 --help
```

Then say which of the three surfaces the problem is on:

- **S3 protocol** — an SDK operation returned the wrong answer, or the wire
  response was wrong. `conformance/` is the contract for this.
- **Run-through** — the cache, the offline path, or propagation to an upstream.
  `/_stow/inspect` reports what the cache holds and what the outbox is doing.
- **Workspace** — `stow-s3 workspace <verb>`. `stow-s3 workspace list` finds an
  existing workspace without needing its id.

## What is not supported

Stated plainly, because these come up:

- **Production object storage.** It is a development and test service. It has no
  versioning, lifecycle, replication, or server-side encryption, and it answers
  `NotImplemented` for all of them rather than pretending.
- **A security boundary for an untrusted agent.** A prepared workspace is a
  directory, not a sandbox. See [SECURITY.md](SECURITY.md).
- **A migration tool for an existing data directory.** An older on-disk layout is
  detected and reported; it is not converted.
- **Multi-tenancy.** One server, one credential pair. There is no per-user
  identity and no isolation between callers beyond the S3 key pair.

## Response

One maintainer, no service-level agreement. A report with a reproduction is
fixed; a report without one waits. If something is blocking you and the tracker
is quiet, that is a real answer about capacity rather than a signal that nobody
read it.

## Before you file anything: check the install surface

Only the Go module is currently installable from a public registry. The npm
packages exist but are not publicly readable, and `stow-s3` is not on PyPI. If
you arrived through an `npm install` or `pip install` that failed, that is
[issue #28](https://github.com/chester-hill-solutions/stow-s3/issues/28) and it
is known.
