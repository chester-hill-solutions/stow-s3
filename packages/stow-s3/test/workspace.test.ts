import assert from "node:assert/strict";
import { chmod, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, it } from "node:test";
import {
  adoptWorkspaceHandoff,
  applyWorkspaceDelta,
  createWorkspaceDelta,
  destroyWorkspace,
  handoffWorkspace,
  prepareWorkspace,
  resumeWorkspace,
  runWorkspaceCommand,
} from "../dist/workspace.js";

describe("workspace CLI adapter", () => {
  it("passes arguments without a shell and parses the JSON response", async () => {
    await withFakeStowBinary(
      "#!/usr/bin/env node\nprocess.stdout.write(JSON.stringify({args: process.argv.slice(2)}));\n",
      async () => {
      const manifest = "/tmp/task manifest.json";
      const result = await prepareWorkspace(manifest);
      assert.deepEqual(result.args, ["workspace", "prepare", "--manifest", manifest]);
      const raw = await runWorkspaceCommand(["diff", "--from", "cp_abc", "--to", "cp_xyz"]);
      assert.deepEqual(raw.args, ["workspace", "diff", "--from", "cp_abc", "--to", "cp_xyz"]);
      },
    );
  });

  it("rejects a non-object JSON result", async () => {
    await withFakeStowBinary("#!/bin/sh\nprintf '[]'\n", async () => {
      await assert.rejects(runWorkspaceCommand(["diff"]), /non-object JSON/);
    });
  });

  it("runs prepare, handoff-to-file, resume, and destroy against the native CLI", async () => {
    const directory = await mkdtemp(join(tmpdir(), "stow-workspace-e2e-"));
    const registryDir = join(directory, "registry");
    const root = join(directory, "workspace");
    const handoffPath = join(directory, "handoff.json");
    const manifestPath = join(directory, "task.json");
    const prior = process.env.STOW_BIN;
    process.env.STOW_BIN = join(process.cwd(), "../../bin/stow-s3");
    try {
      const inputPath = join(directory, "seed.txt");
      await writeFile(inputPath, "seed");
      await writeFile(
        manifestPath,
        JSON.stringify({ version: 1, root, working_directory: ".", registry_dir: registryDir, inputs: [{ source: inputPath, destination: "seed.txt" }] }),
      );
      const prepared = await prepareWorkspace(manifestPath);
      assert.equal(typeof prepared.workspace_id, "string");
      const handoff = await handoffWorkspace(String(prepared.workspace_id), {
        registryDir,
        output: handoffPath,
      });
      assert.deepEqual(JSON.parse(await readFile(handoffPath, "utf8")), handoff);
      const resumed = await resumeWorkspace({ handoffPath });
      assert.equal(resumed.workspace_id, prepared.workspace_id);
      const destroyed = await destroyWorkspace(String(prepared.workspace_id), registryDir);
      assert.equal(destroyed.destroyed, true);
      await assert.rejects(stat(root));
    } finally {
      if (prior === undefined) delete process.env.STOW_BIN;
      else process.env.STOW_BIN = prior;
      await rm(directory, { recursive: true, force: true });
    }
  });
});

describe("workspace delta and handoff", () => {
  it("names both ends and the destination when writing a delta", async () => {
    await withFakeStowBinary(
      "#!/usr/bin/env node\nprocess.stdout.write(JSON.stringify({args: process.argv.slice(2)}));\n",
      async () => {
        const result = await createWorkspaceDelta({
          from: "cp_base",
          to: "cp_target",
          output: "/tmp/change.stowdelta",
          registryDir: "/tmp/registry",
          maxBytes: 4096,
          includeSensitive: true,
        });
        assert.deepEqual(result.args, [
          "workspace",
          "delta",
          "--from",
          "cp_base",
          "--to",
          "cp_target",
          "--output",
          "/tmp/change.stowdelta",
          "--registry-dir",
          "/tmp/registry",
          "--max-bytes",
          "4096",
          "--include-sensitive",
        ]);
      },
    );
  });

  it("never invents the base an applied delta applies to", async () => {
    await withFakeStowBinary(
      "#!/usr/bin/env node\nprocess.stdout.write(JSON.stringify({args: process.argv.slice(2)}));\n",
      async () => {
        const result = await applyWorkspaceDelta({
          delta: "/tmp/change.stowdelta",
          base: "cp_base",
          registryDir: "/tmp/registry",
        });
        assert.deepEqual(result.args, [
          "workspace",
          "apply",
          "--delta",
          "/tmp/change.stowdelta",
          "--base",
          "cp_base",
          "--registry-dir",
          "/tmp/registry",
        ]);
      },
    );
  });

  it("omits limits the caller did not ask for", async () => {
    await withFakeStowBinary(
      "#!/usr/bin/env node\nprocess.stdout.write(JSON.stringify({args: process.argv.slice(2)}));\n",
      async () => {
        const result = await createWorkspaceDelta({ from: "a", to: "b", output: "/tmp/d" });
        assert.deepEqual(result.args, ["workspace", "delta", "--from", "a", "--to", "b", "--output", "/tmp/d"]);
        const applied = await applyWorkspaceDelta({ delta: "/tmp/d", base: "a" });
        assert.deepEqual(applied.args, ["workspace", "apply", "--delta", "/tmp/d", "--base", "a"]);
      },
    );
  });

  it("carries the archive path so a handoff can be adopted elsewhere", async () => {
    await withFakeStowBinary(
      // `handoff --output` writes the document to the named path and prints
      // nothing, and the wrapper reads it back from there. The double has to do
      // the same, or it models a command shape the real binary never has, and the
      // read-back the native-CLI test exercises is never reached here.
      [
        "#!/usr/bin/env node",
        'const a = process.argv.slice(2);',
        'const i = a.indexOf("--output");',
        'if (a[1] === "handoff" && i >= 0) {',
        '  require("node:fs").writeFileSync(a[i + 1], JSON.stringify({args: a}));',
        "  process.exit(0);",
        "}",
        "process.stdout.write(JSON.stringify({args: a}));",
        "",
      ].join("\n"),
      async () => {
        const handed = await handoffWorkspace("ws_1", {
          checkpointId: "cp_1",
          archive: "/tmp/cp.tar.gz",
          output: "/tmp/handoff.json",
        });
        assert.deepEqual(handed.args, [
          "workspace",
          "handoff",
          "--id",
          "ws_1",
          "--checkpoint-id",
          "cp_1",
          "--archive",
          "/tmp/cp.tar.gz",
          "--output",
          "/tmp/handoff.json",
        ]);
        const adopted = await adoptWorkspaceHandoff({
          handoffPath: "/tmp/handoff.json",
          root: "/tmp/adopted",
        });
        assert.deepEqual(adopted.args, [
          "workspace",
          "adopt",
          "--handoff",
          "/tmp/handoff.json",
          "--root",
          "/tmp/adopted",
        ]);
      },
    );
  });
});

async function withFakeStowBinary(script: string, run: () => Promise<void>): Promise<void> {
  const directory = await mkdtemp(join(tmpdir(), "stow-workspace-cli-"));
  const binary = join(directory, "stow-s3");
  const previous = process.env.STOW_BIN;
  try {
    await writeFile(binary, script);
    await chmod(binary, 0o755);
    process.env.STOW_BIN = binary;
    await run();
  } finally {
    if (previous === undefined) {
      delete process.env.STOW_BIN;
    } else {
      process.env.STOW_BIN = previous;
    }
    await rm(directory, { recursive: true, force: true });
  }
}
