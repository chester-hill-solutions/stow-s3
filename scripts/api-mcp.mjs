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

Object references resolve current bytes and do not pin a historical revision.

## Tools

`;
  for (const tool of tools) {
    markdown += `## ${tool.name}\n\n${tool.profile}. ${tool.description}\n\n### Input\n\n\`\`\`json\n${JSON.stringify(tool.inputSchema, null, 2)}\n\`\`\`\n`;
    if (tool.outputSchema) markdown += `\n### Result\n\n\`\`\`json\n${JSON.stringify(tool.outputSchema, null, 2)}\n\`\`\`\n`;
  }
  return { markdown, symbols: tools.map((tool) => tool.name) };
}
