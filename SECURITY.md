# Security Policy

## Reporting a vulnerability

Report privately through GitHub's security advisory form:
**Security → Report a vulnerability** on
[`chester-hill-solutions/stow-s3`](https://github.com/chester-hill-solutions/stow-s3).

Please do not open a public issue for a suspected vulnerability. Include the
output of `stow-s3 --version`, the `doctor --json` report, and the steps that
reproduce it. There is no guaranteed response time; this is a small project with
one maintainer, and a report that arrives with a reproduction gets fixed faster
than one that does not.

## What this software is, and what it runs with

`stow-s3` is a local development and test service. It speaks the S3 wire
protocol, so existing SDK code works against it unchanged, and it stores objects
on the local filesystem. It is **not** production object storage.

The honest description of its blast radius, taken from the code rather than from
intent:

| Property | Where it comes from |
|---|---|
| It holds a serving process with the caller's filesystem and network privileges for as long as it runs | `stow-s3 serve` binds a listener and reads `AWS_*` / `STOW_*` from the environment |
| Anyone who can reach the listener and holds the generated credentials has full S3 access to every bucket it serves | SigV4 is enforced per request (`internal/auth`), but there is one credential pair per server, not per user |
| The default listen address is loopback and the credential pair is generated per process | `--host` defaults to `127.0.0.1`; `main.go` generates credentials when none are given |
| Admin and metrics routes are reachable from loopback without a credential, and the destructive ones always need the admin token | `internal/s3api/server.go` |
| A prepared workspace is a directory, not a sandbox | `internal/storage/workspace` writes the caller's own files; there is no syscall-level isolation |
| Staging credential-looking paths is refused by default | `pkg/stow/prepare.go` skips `.aws/credentials`, `.ssh/`, and gcloud credential paths unless sensitive inputs are explicitly included |

**The workspace is the one to be careful with.** Handing a prepared workspace to
an agent that reads untrusted text hands that agent a directory. Isolation
between workspaces is a naming discipline inside one filesystem, not an OS
boundary. Do not point a workspace at a directory whose contents you would not
want that agent able to read, and do not use it as a containment mechanism for
code you have not reviewed.

Writes that reach a real provider require explicit consent and are off by
default. A run-through server propagates nothing upstream unless both a policy
and `STOW_ALLOW_LIVE_WRITES` are set — see
[`docs/adr/0005-explicit-live-write-consent.md`](docs/adr/0005-explicit-live-write-consent.md).

## Supported versions

Security fixes land on the current minor line only. There is no long-term
support branch; `v0.1.0` and `v0.2.0` predate both of the product's pillars and
should not be deployed.
