---
status: accepted
decision_digest: 445807cb27281b47
relates_to: 0016, 0005, 0011
---

# A bind beyond loopback needs consent, and the service does not terminate TLS

## Context

`serve` binds `127.0.0.1` and accepts any `--host`, so `--host 0.0.0.0` silently
exposed a store and its requests to every host that could reach the port. The
codebase already had this shape for the other direction: an upstream endpoint
beyond loopback needs `--allow-insecure-upstream`
(`internal/runthrough/transport_policy.go`), and ADR 0005 established that
ambient configuration is not a request to act.

ADR 0016 assigns transport and lifecycle to the host adapter: "Host adapters own
bindings, secrets and transport/lifecycle". TLS termination is therefore not the
service's to add, and a decision about exposure has to say what remains the
service's.

## Decision

The service refuses a listen host that is not loopback unless
`--allow-insecure-remote` is passed. The refusal happens before the listener
opens. The flag is consent, not configuration: it says that TLS terminates in
front of this process, or that the operator accepts cleartext for a store they
alone can reach.

A consented non-loopback bind also requires operator-provisioned credentials and
an admin token, from flags or `STOW_LOCAL_ACCESS_KEY_ID`,
`STOW_LOCAL_SECRET_ACCESS_KEY` and `STOW_ADMIN_TOKEN`. A generated pair is
reported only to the process's own stdout, so it cannot authenticate a remote
client, and an absent admin token makes the diagnostic routes answer `404` to
every caller that is not loopback, including a health probe.

The service does not terminate TLS, per ADR 0016. It accepts one credential pair
and one environment authority for all clients. Per-caller principals, scoping and
transports remain ADR 0016's work.

## Consequences

- A misconfigured `--host` fails closed, and the message names the flag that
  resolves it, so the fix is one flag away.
- Deployment documentation has to state the proxy shape rather than describe
  `--host` as sufficient.
- `activate`-time accidents are prevented; a deliberate remote deployment is not
  made harder than the four values it needs.
- Scoped multi-key access stays unimplemented, so a remote deployment currently
  shares one identity across callers.

## Amendments

- 2026-10-23: Accepted the consent flag, the provisioning requirements, and the
  split that leaves TLS termination with the host adapter per ADR 0016.
