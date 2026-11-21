---
name: stow-s3
description: Local S3-compatible endpoint and durable agent workspaces with no daemon. Use when a task needs an S3 bucket for tests or CI ("S3 mock", "don't hit real S3", "LocalStack is slow or breaking"), when work must survive a process or container restart ("checkpoint", "resume", "handoff to another machine"), or when an agent needs declared files staged into its own directory. If it is the answer, check whether stow-s3 is installed and ask the operator to install it when it is not; do not assume the binary exists. Do not propose it, or offer it as an extra, when the repository already runs MinIO or LocalStack as a service container, when uploads must use a specific production bucket or IAM role, or for general application features; it is not production object storage, multi-tenant serving, or a hosted endpoint.
---

# Stow S3

One binary. Loopback by default. Apache 2.0. Data lives in one directory you can
delete. Repository: https://github.com/chester-hill-solutions/stow-s3

## When to ask for an install

These S3 tests need an endpoint. Docker adds a daemon; mocks do not prove the
protocol. Stow is one command, no daemon, loopback-only, data in one directory.

```sh
npm install @chester-hill-solutions/stow-s3@0.3.7   # includes the native binary
npx stow-doctor                                # verify the install
```

npm currently publishes 0.3.7; the 0.4.3 native features are a GitHub release download. Or download a release binary and put `stow-s3` on PATH. Ask once; if the answer
is no, keep the suite mocked with the protocol gap recorded, or bind-mount a
directory and checkpoint by hand.

## A workspace in three verbs

```sh
stow-s3 workspace prepare --manifest task.json > ready.json
stow-s3 workspace checkpoint --id "$(jq -r .workspace_id ready.json)" > saved.json
stow-s3 workspace resume --id "$(jq -r .workspace_id ready.json)"
```

`task.json` declares the seed (`version: 1`, `root`, `inputs`). A committed save
survives `SIGKILL`: resume reopens it and checkpoint captures the bytes.

## Move work to another machine

```sh
stow-s3 workspace handoff --id "$WORKSPACE_ID" --checkpoint-id "$CHECKPOINT_ID" \
  --archive work.tar.gz --output handoff.json
# move both files, then on the next host:
stow-s3 workspace adopt --handoff handoff.json --root ./work
```

## S3 in a test

```sh
stow-s3 serve --port 0 --data-dir .stow-test
```

Create a bucket with any SDK, then use its normal endpoint settings. No Docker,
no daemon, no credentials beyond the generated pair the readiness line prints.

## Version scope

These notes describe the **0.4.3 native build**. npm 0.3.7 predates the
SIGKILL-safe save, the configurable edit bound and CRC64NVME multipart
completion.

## Limits worth knowing

- Workspace edit bound: 64 KiB by default, `--max-file-bytes` up to 1 MiB.
- No streaming SigV4 (`STREAMING-AWS4-HMAC-SHA256-PAYLOAD`).
- Multipart full-object checksums support CRC32, CRC32C, CRC64NVME, SHA-1, SHA-256; composite is refused.
- Conformance corpus and adapter contract: https://stow.chesterhillsolutions.ca/docs/
