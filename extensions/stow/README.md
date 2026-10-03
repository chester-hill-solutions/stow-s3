# Stow MCP browser and plugin

This integration adds a read-only bucket browser to Stow's existing Go stdio MCP
server. It uses the official [MCP Apps](https://github.com/modelcontextprotocol/ext-apps)
and [OpenAI MCP Extensions](https://github.com/openai/mcp-extensions) SDKs. The
browser lists 50 objects per page, previews at most 64 KiB per object (or the
configured lower bound), and attaches up to ten resource references to composer
context on explicit selection. References resolve current object bytes.

## Run the native server

Build this checkout with `make build`. Supply an existing native Stow object-store
directory and its exact bucket; this profile does not adopt arbitrary workspace
files. The process owns that store while connected, so close other Stow processes
using the same directory first.

```sh
/absolute/path/to/stow-s3 mcp \
  --object-dir /absolute/path/to/owned-stow-objects \
  --bucket objects --browser --read-only
```

The Go executable includes the self-contained UI. Running the server requires no
Node process, hosted service, OpenAI API key, or provider credentials. The browser
never writes objects. The `--read-only` profile also refuses the existing save tool
and bucket creation. Deliberate writable configurations can use the existing
[guarded-save workflow](../../docs/portable-workspace-usage.md#native-object-mcp);
the browser remains read-only in such a configuration.

## Codex and local ChatGPT Work

For Codex CLI, register the built executable with explicit scope:

```sh
codex mcp add stow -- /absolute/path/to/stow-s3 mcp \
  --object-dir /absolute/path/to/owned-stow-objects \
  --bucket objects --browser --read-only
```

Ordinary MCP clients can call `stow_browser` and read its returned resources.
Compatible MCP App hosts render the UI. OpenAI extension hosts can expose the
global and thread entrypoints. Composer mention search is a desktop extension
feature; support depends on the host. The UI disables attachment controls when
the host does not advertise model-context updates. Selecting an object reference
does not submit a message.

The `plugin/` directory uses the portable
[Agent Plugins format](https://developers.openai.com/plugins/build/plugins).
Its checked-in `mcp.json` is a template with absolute-path placeholders. Configure
a new copy before installing it from a supported local/repo plugin source:

```sh
node extensions/stow/configure-plugin.mjs \
  /absolute/path/to/stow-s3 /absolute/path/to/owned-stow-objects \
  objects /absolute/path/to/new-stow-plugin
```

This copies the manifest, scoped skill, and a read-only stdio server configuration.
It refuses an existing output directory and does not edit host MCP settings or
register a public plugin. Alternatively, edit a copy of `plugin/mcp.json` manually.
Remote classic ChatGPT integrations require a separately authorized transport
and server registration; this package does not deploy one.

## Build and verify the UI

Node is needed only to develop/build the UI or run the optional configuration
helper. Dependencies are pinned and locked. The gzip artifact is deterministic
and embedded by Go; the MCP resource serves normal HTML with no network assets.
Bundled dependency license texts are included in the HTML and plugin package.

```sh
npm ci --ignore-scripts --prefix extensions/stow
npm run check --prefix extensions/stow
npm test --prefix extensions/stow
npm run build --prefix extensions/stow
node extensions/stow/build.mjs --check
```

Go integration tests cover opt-in registration, metadata, pagination, exact bucket
scope, Unicode keys, preview bounds and absence of save observations. UI host
fixtures cover initial-result handling, plain-text rendering, prefix/cursor
paging, explicit attachments, composer removals, unsupported capabilities and
late preview responses. They do not establish live ChatGPT host compatibility.
