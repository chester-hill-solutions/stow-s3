import { execFileSync } from "node:child_process";

export function mcpReference() {
  const tools = JSON.parse(execFileSync("python3", ["scripts/mcp-api-reference.py"], { encoding: "utf8" }));
  let markdown = `# MCP API

Connect an MCP client to the native server over stdio. The host selects the object directory and bucket, or the workspace registry and ID.
Tool arguments cannot select another backend or widen that scope.

## Object server

\`\`\`sh
stow-s3 mcp --object-dir /absolute/objects --bucket files --create-bucket
\`\`\`

Use \`--read-only\` to reject saves. Add \`--browser\` for browsing and object-reference tools.
Close the connection to release ownership. Object data and save receipts remain.

## Workspace server

\`\`\`sh
stow-s3 mcp --registry-dir /absolute/registry --workspace-id WORKSPACE_ID \\
  --timeout 30s --max-bytes 1073741824 --max-files 100000
\`\`\`

The capture deadline must be positive. Pause writers before capture.
Set \`--export-root\` to enable handoff export. Also set \`--adopt-root\` to enable adoption.
Those directories must already exist. Bundle and destination names are direct children of the configured roots.

## Workspace file resources

Add \`--workspace-files\` to expose current regular files in the configured workspace.
Add \`--resources-only\` to omit tools and prompts; this requires \`--workspace-files\`.
Both flags are incompatible with object mode. The workspace timeout must remain positive.
File resources always provide read-only access, including when checkpoint tools are enabled.
Repeat \`--read-root inputs --read-root reports\` to expose selected existing relative directories.
The default scope is the whole workspace; up to 16 distinct roots are allowed. Mandatory exclusions always apply.
Add \`--browser\` for the optional workspace App and its read-only launcher tool; it requires
\`--workspace-files\` and cannot be combined with \`--resources-only\`.
Readers coexist with the workspace writer and protect registered deletion until close.
All processes removing a workspace with readers attached must support reader leases.
Private paths and symlinks are excluded; resource reads do not create save observations.
Live reads are mutable observations, not checkpoints. See the [workspace resource contract](../contracts/workspace-resources-contract.md).

## Workspace file editing (opt-in)

Add \`--workspace-edit --write-root docs --coordination host_quiesced\` with
\`--workspace-files\` to claim the registered workspace owner and expose the four
managed file tools. Repeat \`--write-root\` to select existing relative directories
within the read scope. The existing owner permissions and storage quotas apply;
capture bounds do not replace them. Quiesce all writers outside this editor.
Object, resource-only, selected-checkpoint and browser modes refuse editing.

Read with \`stow_workspace_file_read_for_save\` to obtain a live observation token.
Persist a random request key and the intended path/body/options before saving.
Use exactly one \`text\` or \`data_base64\` body, and exactly one
\`observation_token\` or \`replace: true\`. File bytes are bounded to 64 KiB.
File resources remain reads; they do not create comparison tokens.

After interruption, restart with the same opt-in settings plus
\`--recover-file-saves\`, then resolve the original path and request key.
Every journal-bearing root requires explicit recovery, including terminal-only
state. Missing receipts and unproven publication remain unknown; never infer
that no write occurred or retry with a new key. Known committed results preserve
the original generation even after later file changes. Checkpoint creation is a
separate caller action after a confirmed save. See the
[workspace editing contract](../contracts/workspace-editing-contract.md).

## Saved checkpoint file resources

Add \`--checkpoint-id CHECKPOINT_ID\` with \`--workspace-files\` to select saved
files instead of the mutable view. The manifest must belong to the configured
workspace. Working files may be absent. \`--resources-only\` still omits tools and
prompts; \`--browser\` refuses this profile. Static read roots and mandatory exclusions
govern file resources, while normal checkpoint tools retain their existing grants.

The view contains captured filesystem files from v1/v2 manifests. Portable logical
objects are not mapped into fictitious paths. Shared read leases protect the
selected checkpoint and registered workspace removal without blocking capture.
An absolute five-minute lifetime actively closes idle readers; reconnect opens a
new view. Process death releases the ephemeral leases without recovery journal rows.

Whole files remain bounded to 64 KiB. A range captures at most 64 KiB while hashing
the entire saved payload, up to 64 MiB, before returning bytes. Corruption outside
the requested window also refuses the read. Follow file \`range_uri\` and range
\`next_uri\` references unchanged. See the [checkpoint resource contract](../contracts/checkpoint-resources-contract.md).

## Limits

| Setting | Value |
| --- | --- |
| Decoded object body | 64 KiB by default; lower with --max-object-bytes |
| Live observations | 256 by default; lower with --max-observations |
| Observation lifetime | 15 minutes |
| Concurrent object calls | 8 |
| Tool arguments / encoded result | 256 KiB each |
| Stdio request line | 1 MiB |
| Workspace inspection page | 100 entries by default; 1–1,000 allowed |
| Bucket browser page | 50 objects |
| Workspace file decoded body / encoded response | 64 KiB / 256 KiB |
| Workspace complete directory resource | 100 visible direct children; 10,000 examined entries; overflow refuses |
| Workspace paged directory observation | 10,000 examined entries / 4 MiB charged metadata; 100 entries and 256 KiB encoded result per page |
| Workspace directory cache | 8 observations / 16 MiB charged metadata; absolute five-minute lifetime |
| Workspace browser HTML / encoded UI response | 2 MiB / 4 MiB; separate from file and directory response limits |
| Saved checkpoint view | 10,000 manifest files / 4 MiB charged metadata; five-minute absolute lifetime |
| Saved whole file / range / full-file verification | 64 KiB / 64 KiB / 64 MiB |
| Concurrent workspace resource reads / deadline | 8 / at most 10 seconds or the configured timeout, whichever is shorter |
| Workspace editing file body / observations / lifetime | 64 KiB / 64 / five minutes |
| Workspace editing arguments / encoded tool result | 256 KiB each; optional observation text is omitted if needed to fit |
| Capture bytes / files | 1 GiB / 100,000 by default; requests can only narrow the host bounds |

These limits do not bound total process memory.

## Save and recovery results

Object results have \`version: 1\` and \`outcome\`. Saves use \`committed\`, \`not_committed\` or \`unknown\`.
Reads, capability checks and observation release use \`ok\`.
\`available\` says whether the requested object or receipt information is included.
\`error\` contains a code and message; MCP \`isError\` is separate from the effect outcome.
A committed outcome remains committed even if response encoding or cleanup reports an error.

1. Persist a unique request_key and the intended key, bytes and options.
2. Read the object to obtain an observation_token, including observed absence.
3. Save with exactly one observation_token or replace: true.
4. After an uncertain reply or server restart, resolve the original key and request_key.

Resolution starts no new save. Missing evidence leaves the outcome unknown.
Observation tokens expire or disappear on restart; receipts remain until explicitly managed.
Receipts retain metadata, not historical bodies. Later reads return current bytes.

Checkpoint results also have \`version: 1\` and an effect outcome.
Only \`committed\` confirms capture. Reuse the original request key and options for resolution.
An error includes code, phase, attempts, retryable and message.
Inspection pages return entries and next_cursor; pass that cursor unchanged to continue.

## Resources

- \`stow://guides/checkpoints/v1\`: checkpoint workflow, text/plain.
- \`ui://stow/browser-v1\`: optional self-contained bucket browser UI.
- \`stow://objects/{bucket}/{key}\`: current bounded object contents; key is a base64url identifier returned by the browser tools.
- \`stow://workspaces/{id}/v1\`: opt-in workspace file overview and navigation roots.
- \`stow://workspaces/{id}/tree/{path}\`: bounded complete direct-child observation; path is canonical base64url of a relative directory; encode \`.\` for the root.
- \`stow://workspaces/{id}/tree-pages/v1/{path}\`: first page of a bounded metadata observation; follow the returned \`page.next_uri\` unchanged.
- \`stow://workspaces/{id}/tree-pages/v1/{path}/{cursor}\`: continuation of that observation; expires after five minutes or cache eviction.
- \`stow://workspaces/{id}/files/{path}\`: bounded current regular-file contents; path is canonical base64url of a relative file.
- \`ui://stow/workspace-browser-v1\`: optional workspace browser UI, bootstrapped to the configured overview.
- \`stow://workspaces/{id}/checkpoints/{checkpoint}/v1\`: selected saved-file overview and root URI.
- \`stow://workspaces/{id}/checkpoints/{checkpoint}/tree/v1/{path}/{offset}\`: saved directory page; omit the offset for its first page.
- \`stow://workspaces/{id}/checkpoints/{checkpoint}/files/{path}\`: bounded complete saved-file contents.
- \`stow://workspaces/{id}/checkpoints/{checkpoint}/ranges/v1/{path}/{offset}/{length}\`: verified saved-file window, returned as versioned JSON text or base64 with complete file digest and size.

The workspace overview supplies \`paged_root_uri\`, selected read roots, capabilities and bounds.
Pages retain directory metadata and ordering, while file reads return current bytes.
They do not establish a snapshot of file contents. Refresh after a cursor expires.

Object references resolve current bytes and do not pin a historical revision.

## Tools

`;
  for (const tool of tools) {
    markdown += `## ${tool.name}\n\n${tool.profile}. ${tool.description}\n\n### Input\n\n\`\`\`json\n${JSON.stringify(tool.inputSchema, null, 2)}\n\`\`\`\n`;
    if (tool.outputSchema) markdown += `\n### Result\n\n\`\`\`json\n${JSON.stringify(tool.outputSchema, null, 2)}\n\`\`\`\n`;
  }
  return { markdown, symbols: tools.map((tool) => tool.name) };
}
