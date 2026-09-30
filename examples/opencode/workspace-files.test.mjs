import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdtemp, mkdir, writeFile, chmod, rm, symlink, realpath } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { agentDirectory, snapshotWorkspace, workspaceChanges } from "./workspace-files.mjs";

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), "stow-task-files-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  return realpath(root);
}

test("agent directory follows readiness cwd and refuses missing or escaped paths", async t => {
  const root = await fixture(t);
  await mkdir(join(root, "repo"));
  assert.equal(await agentDirectory({ root, working_directory: join(root, "repo") }), join(root, "repo"));
  assert.equal(await agentDirectory({ root, working_directory: root }), root);
  await assert.rejects(agentDirectory({ root }), /working_directory/);
  await assert.rejects(agentDirectory({ root, working_directory: "repo" }), /absolute/);
  await symlink(tmpdir(), join(root, "escape"));
  await assert.rejects(agentDirectory({ root, working_directory: join(root, "escape") }), /inside/);
});

test("task evidence detects content, mode, additions and deletions but ignores caller metadata", async t => {
  const root = await fixture(t);
  const options = { maxBytes: 1024, maxFiles: 10 };
  await mkdir(join(root, "repo", ".git"), { recursive: true });
  await mkdir(join(root, ".stow"));
  await writeFile(join(root, "repo", "notes.md"), "before");
  await writeFile(join(root, "deleted.txt"), "delete");
  await writeFile(join(root, "mode.txt"), "mode", { mode: 0o600 });
  const before = await snapshotWorkspace(root, options);
  await writeFile(join(root, "STOW_PROGRESS.json"), "caller progress");
  await writeFile(join(root, "repo", ".git", "config"), "metadata");
  await writeFile(join(root, ".stow", "manifest.json"), "metadata");
  await writeFile(join(root, "repo", "notes.md"), "before");
  assert.deepEqual(workspaceChanges(before, await snapshotWorkspace(root, options)), { added: [], modified: [], deleted: [] });
  await writeFile(join(root, "repo", "notes.md"), "after!");
  await chmod(join(root, "mode.txt"), 0o700);
  await rm(join(root, "deleted.txt"));
  await writeFile(join(root, "new.txt"), "new");
  assert.deepEqual(workspaceChanges(before, await snapshotWorkspace(root, options)), {
    added: ["new.txt"], modified: ["mode.txt", "repo/notes.md"], deleted: ["deleted.txt"],
  });
});

test("task snapshots are bounded, cancellable and refuse symlinks", async t => {
  const root = await fixture(t);
  await writeFile(join(root, "a.txt"), "1234");
  await assert.rejects(snapshotWorkspace(root, { maxBytes: 3, maxFiles: 10 }), /maxBytes/);
  await assert.rejects(snapshotWorkspace(root, { maxBytes: 10, maxFiles: 0 }), /maxFiles/);
  await assert.rejects(snapshotWorkspace(root, { maxBytes: 10, maxFiles: 10, signal: AbortSignal.abort() }));
  await symlink(join(root, "a.txt"), join(root, "link"));
  await assert.rejects(snapshotWorkspace(root, { maxBytes: 10, maxFiles: 10 }), /regular files/);
});
