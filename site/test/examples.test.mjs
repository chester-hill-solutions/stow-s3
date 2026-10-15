import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";

import { buildSite, expandHomeIncludes, includeText } from "../build.mjs";

const site = resolve(import.meta.dirname, "..");
const root = resolve(site, "..");

// The runnable examples the homepage and the guide include. Each is a complete
// program, so a drift between it and the SDK is a broken example a reader would
// copy and run.
const EXAMPLES = [
  "examples/stow-recipes/start-small.mjs",
  "examples/stow-recipes/keep-artifacts.mjs",
  "examples/stow-recipes/processing-stage.mjs",
  "examples/stow-recipes/conditional-write.mjs",
  "examples/stow-recipes/start_small.py",
  "examples/stow-recipes/keep_artifacts.py",
  "examples/stow-recipes/processing_stage.py",
  "examples/stow-recipes/conditional_write.py",
];

const SDK_SOURCE = "packages/stow-s3/src/index.ts";
const PYTHON_SOURCE = "packages/stow-s3-py/src/stow_s3/__init__.py";

const includeMarkers = (html) => [...html.matchAll(/<!--\s*include:([^\s>]+)\s*-->/g)].map((match) => match[1]);

test("every example include resolves and expands away", async () => {
  const home = await readFile(join(site, "index.html"), "utf8");
  const guide = await readFile(join(root, "examples/stow-recipes/README.md"), "utf8");
  for (const source of [home, guide]) {
    const specs = includeMarkers(source);
    assert.ok(specs.length > 0, "no include markers were found, so nothing is being checked");
    for (const spec of specs) {
      assert.ok(includeText(spec).trim().length > 0, `include ${spec} expanded to nothing`);
    }
  }
  assert.equal(includeMarkers(expandHomeIncludes(home)).length, 0, "an include marker survived expansion");
});

test("every example file parses", () => {
  for (const example of EXAMPLES) {
    const path = join(root, example);
    assert.ok(existsSync(path), `${example} is missing`);
    if (example.endsWith(".mjs")) {
      execFileSync("node", ["--check", path]);
      continue;
    }
    if (example.endsWith(".py") && pythonAvailable()) {
      execFileSync("python3", ["-c", "import ast, sys; ast.parse(open(sys.argv[1], encoding='utf-8').read())", path]);
    }
  }
});

test("every example uses symbols the clients export", async () => {
  const sdk = await readFile(join(root, SDK_SOURCE), "utf8");
  const python = await readFile(join(root, PYTHON_SOURCE), "utf8");
  const exportedPython = new Set((python.match(/__all__\s*=\s*\[([\s\S]*?)\]/)?.[1] ?? "").match(/"([^"]+)"/g)?.map((name) => name.slice(1, -1)) ?? []);
  let checked = 0;
  for (const example of EXAMPLES) {
    const source = await readFile(join(root, example), "utf8");
    for (const match of source.matchAll(/import\s*\{([^}]+)\}\s*from\s*"@chester-hill-solutions\/stow-s3"/g)) {
      for (const name of match[1].split(",").map((part) => part.trim().replace(/^type\s+/, "")).filter(Boolean)) {
        assert.match(sdk, new RegExp(`\\b${name}\\b`), `${example} imports ${name}, which ${SDK_SOURCE} does not export`);
        checked += 1;
      }
    }
    for (const match of source.matchAll(/from stow_s3 import ([^\n]+)/g)) {
      for (const name of match[1].split(",").map((part) => part.trim()).filter(Boolean)) {
        assert.ok(exportedPython.has(name), `${example} imports ${name}, which ${PYTHON_SOURCE} does not export`);
        checked += 1;
      }
    }
  }
  assert.ok(checked >= 4, `only ${checked} SDK imports were checked; the examples may have stopped importing the clients`);
});

test("the built homepage carries the example code", async (t) => {
  const output = await mkdtemp(join(tmpdir(), "stow-examples-"));
  t.after(() => rm(output, { recursive: true, force: true }));
  await buildSite(output);
  const home = await readFile(join(output, "index.html"), "utf8");
  assert.doesNotMatch(home, /<!--\s*include:/, "the built homepage still carries an include marker");
  assert.match(home, /publishReport/, "the start-small example is missing from the built homepage");
  assert.match(home, /classifyOne/, "the processing-stage excerpt is missing from the built homepage");
  assert.match(home, /stale update rejected/, "the conditional-write example is missing from the built homepage");
});

function pythonAvailable() {
  try {
    execFileSync("python3", ["--version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}
