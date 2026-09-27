import assert from "node:assert/strict";
import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, it } from "node:test";
import { prepareWorkspace, runWorkspaceCommand } from "../dist/workspace.js";

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
