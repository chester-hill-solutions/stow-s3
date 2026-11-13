# Connect a host to workspace files

This development profile lets an MCP host read the ordinary files of an existing
registered workspace. Build the native executable from this checkout with
`make build`. The connection needs the registry directory and workspace ID from
workspace preparation; it does not adopt an arbitrary directory as an object store.

**Availability:** this is an unreleased 0.4.0 candidate profile. It requires a
binary built from this checkout; published v0.3.7 binaries do not include these
flags or resources. See the [candidate notes](../releases/v0.4.0.md).

```sh
/absolute/path/to/stow-s3 mcp \
  --registry-dir /absolute/registry --workspace-id WORKSPACE_ID \
  --workspace-files --resources-only --read-root inputs --read-root reports \
  --timeout 30s
```

Omit `--read-root` to expose the whole permitted working view. Repeat it for up to
16 distinct existing relative directory roots. Navigation includes ancestors
needed to reach those roots; sibling files remain unavailable. Mandatory private
path exclusions and symlink refusals always apply. Change scope by restarting the
connection. These controls govern this connection's disclosures; they do not
restrict a process that already has filesystem access.

The resource-only profile has no tools or prompts. Omit `--resources-only` to keep
the existing checkpoint tools alongside file resources. File access remains
read-only in either profile. Readers coexist with the workspace writer and prevent
registered removal while attached. Close the server to release its reader lease.

## Navigate resources

Start at `stow://workspaces/WORKSPACE_ID/v1`. Read its `paged_root_uri`, follow
directory entry URIs, and follow `page.next_uri` verbatim until it is absent.
File entry URIs read current regular-file contents. Resource discovery and
directory continuation are separate operations; the host must expose resource
reads to its agent for autonomous navigation.

Each directory observation retains sorted metadata, with at most 10,000 examined
entries and 4 MiB charged metadata. Pages contain at most 100 entries and fit a
256 KiB encoded MCP response. The server caches at most eight observations and
16 MiB charged metadata, expiring them after five minutes or earlier eviction.
If continuation expires, reread the directory's first-page URI. The legacy
`tree/{path}` resource still requires a complete listing of at most 100 children.

Directory metadata and file contents can come from different moments. File reads
return current bytes, bounded to 64 KiB, and refuse oversize reads rather than
truncating them. Use a checkpoint when a stable multi-file basis is required.
Resources do not create managed-save observations or execute file contents.
For separately opted-in managed writes and explicit recovery, use the
[workspace file-save profile](workspace-file-saves.md). Its tools do not change
the read-only behavior of resource reads or resource-only mode.

## Optional browser

For immutable saved files and verified ranges, use the separate
[checkpoint resource profile](checkpoint-resources.md).

Replace `--resources-only` with `--browser` to add `stow_workspace_browser` and
`ui://stow/workspace-browser-v1`. A compatible MCP App host renders the workspace
view from the same self-contained bundle used by the bucket browser. The view
navigates resource pages and previews current bytes. Selecting a reference updates
composer context only when the host supports that capability; it never submits a
message. Browsing remains available when attachments are unsupported.

The UI has its own 2 MiB HTML / 4 MiB encoded-resource budget. This does not raise
file-preview or directory-response limits. The workspace App requires
`--workspace-files` and refuses resource-only mode. The existing object browser
continues to use its own bucket profile.

## Compatibility and qualification

| Caller | Evidence and scope |
| --- | --- |
| Native Go API | Scoped reader and bounded metadata observations; focused race regressions |
| MCP Go client | Resource routing, paging, expiry, confinement and profile regressions |
| Independent TypeScript MCP client | Passed scoped 235-entry traversal against the actual native stdio server |
| OpenCode v2.2.7 | Passed one synthetic resource-only task through OpenRouter Free Router; this does not qualify checkpoint continuation or editing |
| Rendered App SDK bridge fixture | Browser navigation, keyboard, zoom and composer contract verification |
| Live ChatGPT or another App host | Unverified; fixture success does not establish live compatibility |

The [OpenCode Free Router recipe](../../examples/workspace-resources/README.md)
includes a private provider-profile template and an isolated resource-only task.
It uses a guarded free route and synthetic files, with no global host settings changed.

See the [resource contract](../contracts/workspace-resources-contract.md),
[execution plan](../design/workspace-resource-plan.md#w24-execution-backlog) and
[qualification record](../reviews/2026-11-11-workspace-resource-hosts.md) for exact scope and
measured results. The [canonical queue](../development/storage-foundation-plan.md) owns
qualification status. Protected multi-tenant access, strict fresh-model
acceptance and a second physical host remain separate gates. CLI/MCP managed
file saves have their own [local qualification](../reviews/2026-11-12-workspace-editing-mcp.md).
