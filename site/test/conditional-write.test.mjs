import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";
import { pathToFileURL } from "node:url";

const site = resolve(import.meta.dirname, "..");
const root = resolve(site, "..");
const mocks = join(site, "test", "recipe-mocks");
const recipes = join(root, "examples/stow-recipes");

// A recipe has to fail when the service misbehaves, and it has to pass when
// the service behaves. These checks run the published recipe bodies against a
// scripted service, so a recipe that swallows an unrelated failure, or that
// does not look at what was actually stored, fails here.
//
// The recipe bodies are not changed. The JavaScript recipe's two imports are
// redirected to the mock modules, and the Python recipe finds the mock
// package on PYTHONPATH, so each runs exactly as it is published.

const SCENARIOS = {
  // The service refuses the stale write for the right reason and keeps the
  // version that was saved.
  good: "stale-refused",
  // The service refuses the request for an unrelated reason.
  unrelated: "access-denied",
  // The service refuses the stale write correctly but stores something else.
  wrongContent: "wrong-retained",
};

async function runJavaScript(scenario) {
  const source = await readFile(join(recipes, "conditional-write.mjs"), "utf8");
  const patched = source
    .replace('"@aws-sdk/client-s3"', JSON.stringify(pathToFileURL(join(mocks, "aws.mjs")).href))
    .replace('"@chester-hill-solutions/stow-s3"', JSON.stringify(pathToFileURL(join(mocks, "stow.mjs")).href));
  const directory = await mkdtemp(join(tmpdir(), "stow-recipe-"));
  try {
    const file = join(directory, "recipe.mjs");
    await writeFile(file, patched);
    return run("node", [file], scenario);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}

async function runPython(scenario) {
  const directory = await mkdtemp(join(tmpdir(), "stow-recipe-"));
  try {
    return run("python3", [join(recipes, "conditional_write.py")], scenario, {
      PYTHONPATH: join(mocks, "python"),
    });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}

function run(command, args, scenario, extra = {}) {
  try {
    const output = execFileSync(command, args, {
      env: { ...process.env, ...extra, STOW_RECIPE_SCENARIO: scenario },
      stdio: "pipe",
    });
    return { failed: false, output: String(output) };
  } catch (error) {
    return {
      failed: true,
      output: `${error.stdout ?? ""}${error.stderr ?? ""}`,
    };
  }
}

test("the conditional-write recipe passes when the refusal is right and the content is kept", async () => {
  const result = await runJavaScript(SCENARIOS.good);
  assert.equal(result.failed, false, `the recipe failed on the expected path:\n${result.output}`);
});

test("the conditional-write recipe fails when the service refuses for another reason", async () => {
  const result = await runJavaScript(SCENARIOS.unrelated);
  assert.equal(result.failed, true, "the recipe accepted a refusal that was not a precondition failure");
});

test("the conditional-write recipe fails when the stored object is not the saved version", async () => {
  const result = await runJavaScript(SCENARIOS.wrongContent);
  assert.equal(result.failed, true, "the recipe accepted a stored object that was not the version it saved");
});

test("the python conditional-write recipe passes when the refusal is right and the content is kept", async (t) => {
  if (!pythonAvailable()) {
    t.skip("python3 is not available on this host");
    return;
  }
  const result = await runPython(SCENARIOS.good);
  assert.equal(result.failed, false, `the recipe failed on the expected path:\n${result.output}`);
});

test("the python conditional-write recipe fails when the service refuses for another reason", async (t) => {
  if (!pythonAvailable()) {
    t.skip("python3 is not available on this host");
    return;
  }
  const result = await runPython(SCENARIOS.unrelated);
  assert.equal(result.failed, true, "the recipe accepted a refusal that was not a precondition failure");
});

test("the python conditional-write recipe fails when the stored object is not the saved version", async (t) => {
  if (!pythonAvailable()) {
    t.skip("python3 is not available on this host");
    return;
  }
  const result = await runPython(SCENARIOS.wrongContent);
  assert.equal(result.failed, true, "the recipe accepted a stored object that was not the version it saved");
});

function pythonAvailable() {
  try {
    execFileSync("python3", ["--version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}
