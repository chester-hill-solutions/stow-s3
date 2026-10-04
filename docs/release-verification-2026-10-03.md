# 0.3.0 verification checkpoint — 2026-10-03

Candidate: `0.3.0`, merged main `b1186fbccfea6c21b20e921bd6746ec7d8b17557`.
[Post-merge CI](https://github.com/chester-hill-solutions/stow-s3/actions/runs/37152033120)
passed all 13 jobs. No tag, deployment or registry publication was performed.

## Release rehearsal and optional PyPI correction

[Rehearsal 37152770955](https://github.com/chester-hill-solutions/stow-s3/actions/runs/37152770955)
passed 12 of 13 jobs: full tests/standards/generated checks, four native builds,
four wheels and four packed npm consumers. The final job failed at **Preflight
exact npm artifacts** with `npm registry lookup failed: E401`. Later checks,
including the final checksum step, were skipped. Live-provider jobs were dry runs;
they supply no executed-provider evidence for a release.

The tag-only anonymous-consumer step previously always installed
`stow-s3==$version` from PyPI, while PyPI publication was optional. With publishing
disabled, this could fail after npm packages had already published. The correction
splits npm and Python verification: npm remains required on every release tag,
and Python uses exactly the same tag/enabled condition as **Publish to PyPI**.
Each consumer copies its driver into its own temporary directory.

`scripts/release-workflow.test.mjs` checks the real workflow, including absence of
Python installation in the unconditional npm step and coverage of every exact
PyPI registry install. It fails against the previous workflow and passes with the
correction. No authentication, registry destination or publication gate is changed.

## npm authentication blocker: observations and user decision

- The chosen destination in all five manifests and the publish script is
  `https://npm.pkg.github.com`.
- **Set up Node** creates the registry configuration. **Preflight exact npm
  artifacts** receives no `NODE_AUTH_TOKEN`. The only workflow bindings for that
  token are on the later publishing steps, from `secrets.NPM_TOKEN`.
- Read-only repository metadata showed no repository secrets or variables.
  `STOW_NPM_PUBLISH_READY` and `STOW_PUBLISH_PYPI` are unset. Organization/environment
  secrets were not inspected, so their existence is unknown.
- Workflow permissions grant `contents: write` and `id-token: write`, with no
  package permission. The preflight is therefore neither wired to a supplied token
  nor to a package-capable workflow token.
- All five unauthenticated package requests returned HTTP 401. Package metadata
  lookup returned HTTP 403: the inspecting account lacks `read:packages`. Package
  visibility and repository association remain **unverified**.

An earlier report interpreted 401 as proof of private visibility. That conclusion
is withdrawn: [GitHub's npm registry documentation](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry)
requires authentication for public packages too. Changing visibility alone is not
a supported way to satisfy this workflow's anonymous-install requirement.

The smallest necessary user action is to resolve that distribution-policy conflict:

1. **Keep anonymous installation:** authorize a registry that supports it, such as
   npmjs, and its publisher setup before a separately reviewed registry change.
2. **Keep GitHub Packages:** authorize authenticated distribution and revised
   consumer/visibility gates. For preflight, prefer a repository `GITHUB_TOKEN` with
   `packages: read` and package Actions access granted to this repository. Publishing
   needs package write permission; the current workflow instead expects `NPM_TOKEN`.

GitHub documents repository-linked packages and `GITHUB_TOKEN` as a supported Actions
path. Existing package association/access must be confirmed by an owner. No credential
was generated, displayed or configured. Setting the ready variable alone does not
resolve preflight or anonymous consumption. Executed live receipts and exact published
consumer checks also remain release gates.

## Real Codex client: passed reads, scoped approval still needed

Codex CLI `0.159.2`, using existing ChatGPT login, ran with `--ignore-user-config`,
`--ephemeral`, a read-only sandbox and one invocation-only MCP server. The server
command was `bin/stow-s3 mcp --object-dir <disposable-store> --bucket smoke-objects
--browser --read-only`. Personal configuration and other sessions were untouched.

Actual tool evidence passed 50+1 pagination, ordinary/Unicode/empty resource reads,
oversized/missing/cross-bucket rejection, unchanged note bytes and a second fresh
process connection. Binary preview and desktop UI flows were not tested by that client.

Two calls stopped at host policy **before reaching Stow**:

```json
{"tool":"stow_object_capabilities","arguments":{}}
{"tool":"stow_object_save","arguments":{"key":"notes/00.txt","data_base64":"bm8=","replace":true,"request_key":"smoke-denial"}}
```

Both returned `MCP tool call requires approval, but approval policy is never`.
The save test therefore proves host refusal, not Stow's read-only denial. Server
regressions separately cover Stow denial.

Required user action: use an interactive, isolated test session whose host can
present approvals, with per-tool `approval_mode = "prompt"` for only those two
tools. Approve each displayed call once against the synthetic `smoke-objects`
server, retaining `--read-only`. Expect capabilities to return and save to return
Stow `denied`, then reread the note. Do not apply persistent blanket approval.
[Supported MCP tool policy](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)
includes per-tool prompts. No approval override was applied during verification.

## Short manual ChatGPT desktop check

Use ChatGPT Work on a local desktop executor with STDIO MCP and local plugin sources.
Composer mentions require the [desktop extension host](https://developers.openai.com/plugins/build/extensions).
The verification machine has a scoped plugin at `/tmp/stow-host-smoke/plugin`,
pointing only to its synthetic store. The reusable source is
[`extensions/stow/plugin`](../extensions/stow/plugin); the
[configuration helper](../extensions/stow/README.md#codex-and-local-chatgpt-work)
creates a new scoped copy for another machine.

1. Review that copy's `mcp.json`: exact disposable directory/bucket, `--browser`,
   `--read-only`. Add it to a supported local marketplace and install **Stow Storage**
   from the Plugins Directory; start a new local chat with it enabled.
2. Open the Stow sidebar/thread entrypoint. Search `notes/`, page 50+1 items, and
   preview a note, Unicode key and empty object. A large or wrong-bucket preview
   must disclose no body.
3. Explicitly select a note reference; check its composer attachment and removal.
   Selecting must not submit a message. Test desktop mention search if exposed.
4. Reconnect/reopen and reread; bytes must remain unchanged. Use the two one-shot
   approvals above only in this synthetic session to test Stow save denial.
5. Disable/remove only this test plugin after recording the result.

[OpenAI's local-plugin test flow](https://developers.openai.com/plugins/deploy/connect-chatgpt)
uses a local marketplace, installation and a new conversation. Local desktop MCP
can also be inspected in Settings → MCP servers. If the host lacks a local source,
stop and report that limitation. Hosted/web testing would require a separately
authorized connection or tunnel; none was created. Computer Use explicitly prohibited
access to `com.openai.codex`, so sidebar, preview and composer compatibility remain
unrun rather than inferred from browser fixtures.
