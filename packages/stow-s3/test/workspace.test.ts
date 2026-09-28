import assert from "node:assert/strict";
import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, it } from "node:test";
import {
  adoptWorkspaceHandoff,
  applyWorkspaceDelta,
  createWorkspaceDelta,
  handoffWorkspace,
  prepareWorkspace,
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
      "#!/usr/bin/env node\nprocess.stdout.write(JSON.stringify({args: process.argv.slice(2)}));\n",
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
