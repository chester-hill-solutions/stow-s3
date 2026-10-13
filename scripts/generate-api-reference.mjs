import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { apiNotes, operationNotes, fieldNotes } from "../site/api-notes.mjs";
import { firstSentence, typescriptCatalog } from "./api-typescript.mjs";
import { mcpReference } from "./api-mcp.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
process.chdir(root);
const tableCell = (text) => String(text ?? "").replaceAll("|", "\\|").replaceAll("\n", " ");
const slug = (name) => name.toLowerCase().replaceAll(/[^\p{L}\p{N}_\s-]/gu, "").replaceAll(/\s/g, "-");
const code = (language, text) => `\n\`\`\`${language}\n${text.trim()}\n\`\`\`\n`;
const parameterMeaning = (p) => p.name === "metadata" && p.type === "string" ? "Serialized binary-bridge request or response metadata." : fieldNotes[p.name] ?? fieldNotes[p.name.replace(/_([a-z])/g, (_, letter) => letter.toUpperCase())] ?? "";
const methods = {
  ReadForSave: "Read bytes and metadata with an opaque condition, including observed absence. The condition grants no permission.",
  SaveObject: "Compare the observation at publication. Inspect Outcome even when an error is returned. Qualified filesystem stores retain keyed receipts.",
  ResolveSave: "Resolve the original request key without starting another save. Missing evidence leaves the effect unknown.",
  ReplacementCondition: "Request unconditional replacement while preserving authorization and quota checks.",
  Facade: "Start or reuse a loopback S3 endpoint over this workspace's runtime.",
  FacadeWithOptions: "Start the workspace S3 facade with explicit request and concurrency limits.",
  RefreshUsage: "Reconcile direct filesystem writes at a quiescent boundary before quota-sensitive operations.",
  Destroy: "Delete only an eligible owned workspace with EnvironmentDestroy authority. Adopted and protected directories are refused.",
  CaptureCheckpoint: "Capture paused writers with a persisted unique request key. Replay retained results before admitting a new capture.",
  ResolveCheckpoint: "Resolve an existing capture request without making another checkpoint.",
  prepareWorkspace: "Create a workspace from a task manifest and return its launch descriptor.",
  checkpointWorkspace: "Capture an immutable checkpoint of paused writers.",
  resumeWorkspace: "Open the workspace selected by ID or a same-machine handoff.",
  runWorkspaceCommand: "Run the native workspace command without a shell and decode its JSON object.",
  listWorkspaces: "List registered workspaces, including records whose directories are missing.",
  pruneWorkspaces: "Remove registry records for missing directories; preserve existing directories.",
  collectWorkspaces: "Collect expired workspaces that are unused and eligible for deletion.",
  destroyWorkspace: "Explicitly destroy a registered, eligible workspace.",
  handoffWorkspace: "Create a handoff reference, optionally with a portable archive.",
  adoptWorkspaceHandoff: "Verify a portable handoff and restore it under a new workspace identity.",
  createWorkspaceDelta: "Write a verified change document between two checkpoints from the same workspace.",
  applyWorkspaceDelta: "Verify the named base and optional document digest before publishing a new checkpoint.",
  diffWorkspaces: "Compare two checkpoints and return changed paths.",
  restoreWorkspaceCheckpoint: "Restore a checkpoint into a new workspace directory.",
  exportWorkspaceCheckpoint: "Write a portable checkpoint archive to the selected file.",
  importWorkspaceCheckpoint: "Validate and import a portable checkpoint archive.",
  serveWorkspace: "Own a workspace S3 server until the returned handle closes; preserve workspace data.",
  Stow: "Server startup, endpoint connection and AWS SDK configuration helpers.",
  default: "The same Stow helper object as the named export.",
  start: "Start a native server and create the requested buckets before returning its handle.",
  connect: "Create an owned SDK client for an existing endpoint; disconnect it when finished.",
  awsSdkV3Config: "Return AWS SDK connection settings with local credentials and path-style addressing by default.",
  openStow: "Start a private S3 session and return its owned handle. Call close when finished.",
  withStow: "Run the callback in a private session and always close it.",
  open_session: "Start a private native S3 server. Call Session.close when finished.",
  with_session: "Use as a context manager; exit stops the server and removes only session-owned data.",
  s3_client: "Build a boto3 client from this session's endpoint and credentials. boto3 must be installed separately.",
  new_bucket_name: "Return a collision-resistant bucket name without creating a bucket.",
  build_child_env: "Copy the process environment, remove STOW_*, S3_* and AWS_*, then apply the supplied extra values.",
  migrateFileHistory: "Copy retained legacy stages into a dedicated project without changing the source or discarding uncertain sync attempts.",
};
const notes = (item) => ({
  DEFAULT_SESSION_MAX_BYTES: "Default session storage limit: 16 MiB.",
  DEFAULT_SESSION_MAX_OBJECTS: "Default session object count: 1,000.",
  BROWSER_PERSISTENCE_FORMAT_VERSION: "Supported IndexedDB object format version: 1.",
}[item.name]) ?? methods[item.name] ?? operationNotes[item.name] ?? operationNotes[item.name[0]?.toLowerCase() + item.name.slice(1)] ?? firstSentence(item.description ?? "");

function callable(item, language, owner) {
  let result = `\n### ${owner ? `${owner}.` : ""}${item.name}\n\n${notes(item)}\n${code(language, item.signature)}`;
  if (item.parameters?.length) {
    result += "\n| Parameter | Type | Required | Default | Meaning |\n| --- | --- | --- | --- | --- |\n";
    for (const p of item.parameters) result += `| \`${tableCell(p.name)}\` | \`${tableCell(p.type)}\` | ${p.optional ? "No" : "Yes"} | ${tableCell(p.default ?? "—")} | ${tableCell(parameterMeaning(p))} |\n`;
  }
  if (item.returns) result += `\nReturns: \`${item.returns}\`.\n`;
  return result;
}

function renderSymbols(entries, language) {
  let result = "\n## Symbols\n\n" + entries.map((e) => `[${e.name}](#${slug(e.name)})`).join(" · ") + "\n";
  for (const item of entries) {
    result += `\n## ${item.name}\n\n${notes(item)}\n${code(language, item.signature)}`;
    if (item.fields?.length) {
      result += "\n| Field | Type | Optional | Description |\n| --- | --- | --- | --- |\n";
      for (const field of item.fields) result += `| \`${tableCell(field.name)}\` | \`${tableCell(field.type)}\` | ${field.optional ? "Yes" : "No"} | ${tableCell(field.description)} |\n`;
    }
    for (const method of item.methods ?? []) result += callable(method, language, item.name);
    if (item.kind === "function" && item.parameters) {
      result += "\n| Parameter | Type | Required | Default | Meaning |\n| --- | --- | --- | --- | --- |\n";
      for (const p of item.parameters) result += `| \`${tableCell(p.name)}\` | \`${tableCell(p.type)}\` | ${p.optional ? "No" : "Yes"} | ${tableCell(p.default ?? "—")} | ${tableCell(parameterMeaning(p))} |\n`;
      result += `\nReturns: \`${item.returns}\`.\n`;
    }
  }
  return result;
}

function intro(note, language = "ts") {
  let text = `# ${note.title}\n\n${note.intro}\n\n## Defaults and limits\n\n| Setting | Value |\n| --- | --- |\n`;
  for (const [key, value] of note.defaults) text += `| ${key} | ${value} |\n`;
  return text + `\n## Behavior\n\n${note.behavior.split(/(?<=\.)\s/).map((line) => "- " + line).join("\n")}\n\n## Errors\n\n${note.errors.split(/(?<=\.)\s/).map((line) => "- " + line).join("\n")}\n\n## Example\n${code(language, note.example)}`;
}

export function generateReferences() {
  const files = new Map();
  const inventory = [];
  for (const [entry, symbols] of Object.entries(typescriptCatalog())) {
    const note = apiNotes[entry];
    if (!note) throw new Error(`Missing API notes for ${entry}`);
    const path = `docs/api/${note.slug}.md`;
    files.set(path, intro(note) + renderSymbols(symbols, "ts"));
    inventory.push({ path, title: note.title, symbols: symbols.map((s) => s.name), members: symbols.flatMap((s) => (s.methods ?? []).map((m) => `${s.name}.${m.name}`)) });
  }
  const go = JSON.parse(execFileSync("go", ["run", "./tools/api-reference"], { encoding: "utf8" })).sort((a, b) => a.name.localeCompare(b.name));
  const goNote = {
    title: "Go API", intro: "Import `github.com/chester-hill-solutions/stow-s3/pkg/stow`. Use Runtime for objects and Workspace for editable directories and checkpoints.",
    defaults: [["Open", "Memory when no Store is supplied"], ["MaxBytes / MaxObjects", "Zero means unlimited"], ["MaxMultipartUploads", "Zero selects 1,024; negative values are invalid"], ["Authority", "nil permits all operations; ReadWrite excludes upstream and destruction"], ["ListOptions.Limit", "Zero selects 1,000 objects per page; negative values are invalid"], ["CheckpointArchiveOptions", "Defaults to 1 GiB / 100,000 files"]],
    behavior: "Pass a context to operations for cancellation. Check Capabilities before optional operations. ETag conditions compare content; ReadForSave/SaveObject also detect managed metadata and generation changes. Save results distinguish SaveCommitted, SaveNotCommitted and SaveUnknown. Persist request keys before durable saves or captures. Resolve uncertain results before retrying. Close preserves durable data; Destroy is a separate authorized operation. Workspace embeds Runtime and inherits its object methods. Store extensions advertise conditional, staged, streaming, range and multipart support independently.",
    errors: "Use errors.Is for sentinel errors and errors.As for structured errors. ErrPreconditionFailed and ErrSaveConflict preserve current data. ErrMutationCommitted means content was published despite a later index error; inspect the returned object before retrying. ErrSaveRequestUnknown and SaveUnknown require resolution. ErrQuotaExceeded bounds managed storage, not process memory or direct host writes.",
    example: 'runtime, err := stow.Open(stow.Options{MaxBytes: 16 << 20, MaxObjects: 1000})\nif err != nil { return err }\ndefer runtime.Close()\nctx := context.Background()\nif err := runtime.CreateBucket(ctx, "files"); err != nil { return err }\nobject, err := runtime.PutObject(ctx, "files", "note.txt", []byte("hello"), stow.PutOptions{ContentType: "text/plain"})\nif err != nil { return err }\n_ = object',
  };
  files.set("docs/api/go.md", intro(goNote, "go") + renderSymbols(go, "go"));
  inventory.push({ path: "docs/api/go.md", title: goNote.title, symbols: go.map((s) => s.name), members: go.flatMap((s) => (s.methods ?? []).map((m) => `${s.name}.${m.name}`)) });
  const python = JSON.parse(execFileSync("python3", ["scripts/python-api-reference.py"], { encoding: "utf8" })).sort((a, b) => a.name.localeCompare(b.name));
  const pyNote = {
    title: "Python API", intro: "Import session, workspace and binary helpers from `stow_s3`. All operations are synchronous.",
    defaults: [["max_bytes / max_objects", "16 MiB / 1,000 objects; zero means unlimited"], ["max_request_bytes / max_concurrent_requests", "8 MiB / 16; supplied values must be positive integers"], ["data_dir", "A session-owned temporary directory"], ["Session startup timeout", "15 seconds"], ["serve_workspace timeout", "15 seconds"], ["Archives", "1 GiB / 100,000 files"]],
    behavior: "with_session is a context manager. open_session returns a Session you must close. Sessions create no buckets: call s3.create_bucket explicitly. new_bucket_name only returns a name. A supplied data_dir remains on close. Workspace helpers return dictionaries from the native command. ServedWorkspace supports with blocks and preserves workspace data on exit. Keep registry_dir and team consistent. Resolve uncertain capture replies with the original request key and options.",
    errors: "Negative limits and invalid positive request limits raise ValueError. StowBinaryNotFoundError exposes code and the attempted path. StowProtocolError exposes code for readiness failures. WorkspaceCommandError exposes command, returncode and stderr. s3_client raises ImportError when boto3 is unavailable. S3 operations raise the selected SDK's errors.",
    example: 'from stow_s3 import with_session\n\nwith with_session() as session:\n    s3 = session.s3_client()\n    bucket = session.new_bucket_name()\n    s3.create_bucket(Bucket=bucket)\n    s3.put_object(Bucket=bucket, Key="note.txt", Body=b"hello")\n    data = s3.get_object(Bucket=bucket, Key="note.txt")["Body"].read()',
  };
  files.set("docs/api/python.md", intro(pyNote, "python") + renderSymbols(python, "python"));
  inventory.push({ path: "docs/api/python.md", title: pyNote.title, symbols: python.map((s) => s.name), members: python.flatMap((s) => (s.methods ?? []).map((m) => `${s.name}.${m.name}`)) });
  const mcp = mcpReference();
  files.set("docs/api/mcp.md", mcp.markdown);
  inventory.push({ path: "docs/api/mcp.md", title: "MCP API", symbols: mcp.symbols, members: [] });
  files.set("docs/api/README.md", "# API reference\n\nChoose the interface used by your application. Each reference lists public functions, methods, input types, return types and errors.\n\n" + inventory.map((p) => `- [${p.title}](${p.path.split("/").pop()})`).join("\n") + "\n- [S3 HTTP API](../compat-contract.md): routes, headers, conditions, responses and unsupported operations.\n");
  files.set("docs/api/inventory.json", JSON.stringify(inventory, null, 2) + "\n");
  return files;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const files = generateReferences();
  mkdirSync("docs/api", { recursive: true });
  for (const [path, content] of files) {
    if (process.argv.includes("--check")) {
      if (readFileSync(path, "utf8") !== content) throw new Error(`API reference is stale: ${path}`);
    } else writeFileSync(path, content);
  }
  console.log(`API reference ${process.argv.includes("--check") ? "verified" : "written"}: ${files.size - 1} pages`);
}
