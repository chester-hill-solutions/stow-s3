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

test("anonymous npm verification stays required on every release tag", () => {
  const npm = step("Verify exact published npm versions as anonymous consumers");
  assert.equal(condition(npm), "startsWith(github.ref, 'refs/tags/v')");
  assert.match(npm, /npm install .*--userconfig=.*stow-s3@\$version/);
  assert.match(npm, /node smoke-npm-consumer\.mjs "\$version"/);
  assert.doesNotMatch(npm, /pip install|smoke-python-consumer/);
});

test("anonymous Python verification has exactly the PyPI publication condition", () => {
  const python = step("Verify exact published PyPI version as anonymous consumer");
  const publish = step("Publish to PyPI");
  assert.equal(condition(python), condition(publish));
  assert.equal(condition(python), "startsWith(github.ref, 'refs/tags/v') && steps.pypi.outputs.status == 'enabled'");
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
