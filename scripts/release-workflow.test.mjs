import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const workflow = readFileSync(new URL("../.github/workflows/release.yml", import.meta.url), "utf8");
const steps = workflow.split(/(?=^      - name: )/m);

function step(name) {
  const found = steps.find((block) => block.startsWith(`      - name: ${name}\n`));
  assert.ok(found, `missing release step: ${name}`);
  return found;
}

function condition(block) {
  const found = block.match(/^        if: (.+)$/m);
  assert.ok(found, "anonymous registry verification must have an explicit condition");
  return found[1];
}

test("anonymous npm verification stays required when registry publication is selected", () => {
  const npm = step("Verify exact published npm versions as anonymous consumers");
  assert.equal(condition(npm), "startsWith(github.ref, 'refs/tags/v') && needs.plan.outputs.publish_packages == 'true'");
  assert.match(npm, /npm install .*--userconfig=.*stow-s3@\$version/);
  assert.match(npm, /node smoke-npm-consumer\.mjs "\$version"/);
  assert.doesNotMatch(npm, /pip install|smoke-python-consumer/);
});

test("anonymous Python verification has exactly the PyPI publication condition", () => {
  const python = step("Verify exact published PyPI version as anonymous consumer");
  const publish = step("Publish to PyPI");
  assert.equal(condition(python), condition(publish));
  assert.equal(condition(python), "startsWith(github.ref, 'refs/tags/v') && needs.plan.outputs.publish_packages == 'true' && steps.pypi.outputs.status == 'enabled'");
  assert.match(python, /pip install --index-url=https:\/\/pypi\.org\/simple "stow-s3==\$version"/);
  assert.match(python, /python-consumer\/bin\/python smoke-python-consumer\.py/);
  assert.match(python, /version="\$\{RELEASE_VERSION#v\}"/);
  assert.match(python, /cp scripts\/smoke-python-consumer\.py/);
  assert.doesNotMatch(python, /npm install/);
});

test("all exact PyPI registry installs are gated by successful optional publication", () => {
  const installs = steps.filter((block) => block.includes('pip install --index-url=https://pypi.org/simple "stow-s3==$version"'));
  assert.equal(installs.length, 1);
  assert.equal(condition(installs[0]), condition(step("Publish to PyPI")));
});

test("every registry publish and consumer step is gated by the explicit distribution plan", () => {
  for (const name of ["Publish platform packages", "Publish npm package", "Publish to PyPI", "Verify exact published npm versions as anonymous consumers", "Verify exact published PyPI version as anonymous consumer"]) {
    assert.match(condition(step(name)), /needs\.plan\.outputs\.publish_packages == 'true'/);
    assert.match(condition(step(name)), /startsWith\(github\.ref, 'refs\/tags\/v'\)/);
  }
  for (const name of ["Require release account setup", "Preflight exact npm artifacts", "Download live provider receipts"]) {
    assert.equal(condition(step(name)), "needs.plan.outputs.publish_packages == 'true'");
  }
});

test("downloads retain build, test and consumer gates when live-provider job is skipped", () => {
  const release = workflow.slice(workflow.indexOf("  release:"));
  for (const job of ["plan", "build", "test", "consumer"]) assert.ok(release.includes(`needs.${job}.result == 'success'`));
  assert.match(release, /needs\.plan\.outputs\.publish_packages == 'false' \|\| needs\.live_gate\.result == 'success'/);
  assert.equal(condition(step("Publish release")), "startsWith(github.ref, 'refs/tags/v')");
  assert.match(step("Publish release"), /body_path: \$\{\{ needs\.plan\.outputs\.notes_file \}\}/);
});
