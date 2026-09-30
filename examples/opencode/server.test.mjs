import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdtemp, mkdir, writeFile, readFile, rm, realpath } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { startOpenCode } from "./server.mjs";

test("version and serving processes both use isolated OpenCode directories", async t => {
  const root = await mkdtemp(join(tmpdir(), "stow-opencode-start-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const binary = join(root, "opencode");
  const directory = join(root, "workspace");
  const stateDir = join(root, "state");
  const original = { OPENCODE_API_KEY: process.env.OPENCODE_API_KEY, OPENCODE_CONFIG: process.env.OPENCODE_CONFIG };
  process.env.OPENCODE_API_KEY = "test-only-credential";
  process.env.OPENCODE_CONFIG = "ambient-config-must-not-leak";
  t.after(() => {
    for (const [key, value] of Object.entries(original)) {
      if (value === undefined) delete process.env[key]; else process.env[key] = value;
    }
  });
  await mkdir(directory);
  await writeFile(binary, `#!${process.execPath}
const fs = require('node:fs');
fs.appendFileSync(${JSON.stringify(join(root, "calls.jsonl"))}, JSON.stringify({ args: process.argv.slice(2), home: process.env.HOME, data: process.env.XDG_DATA_HOME, xdgConfig: process.env.XDG_CONFIG_HOME, cache: process.env.XDG_CACHE_HOME, state: process.env.XDG_STATE_HOME, credentialForwarded: process.env.OPENCODE_API_KEY === 'test-only-credential', config: process.env.OPENCODE_CONFIG, cwd: process.cwd() }) + '\\n');
if (process.argv.includes('--version')) { console.log('opencode v2.0.16'); process.exit(0); }
console.log('server listening on http://127.0.0.1:12345');
console.log('server password test-password');
setInterval(() => {}, 1000);
`, { mode: 0o700 });
  const server = await startOpenCode({ binary, stateDir, directory });
  t.after(() => server.close());
  assert.equal(server.endpoint, "http://127.0.0.1:12345");
  assert.equal(server.password, "test-password");
  const calls = (await readFile(join(root, "calls.jsonl"), "utf8")).trim().split("\n").map(JSON.parse);
  assert.equal(calls.length, 2);
  for (const call of calls) {
    assert.equal(call.home, join(stateDir, "opencode", "home"));
    assert.equal(call.data, join(stateDir, "opencode", "home", ".local", "share"));
    assert.equal(call.xdgConfig, join(call.home, ".config"));
    assert.equal(call.cache, join(call.home, ".cache"));
    assert.equal(call.state, join(call.home, ".local", "state"));
    assert.equal(call.credentialForwarded, true);
    assert.equal(call.config, undefined);
    assert.equal(call.cwd, await realpath(directory));
  }
  await server.close();
});
