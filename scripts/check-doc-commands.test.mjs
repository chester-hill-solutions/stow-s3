import assert from "node:assert/strict";
import { test } from "node:test";

import {
  codeSpans,
  commandProblems,
  commandSurface,
  flagProblems,
  invocations,
  staleExemptions,
  EXEMPT,
} from "./check-doc-commands.mjs";

// A hand-built surface, so the rules below are tested against a known truth
// rather than against whatever the binary currently reports. A test that reads
// the real surface cannot fail for the reason it exists: the surface and the
// document move together, and a document naming a removed verb would also remove
// the verb.
const SURFACE = {
  commands: new Set(["serve", "doctor", "workspace", "prewarm"]),
  verbs: new Set(["prepare", "resume", "list", "handoff"]),
  flags: new Set(["port", "data-dir", "json", "help", "h"]),
};

const NONE = {};

test("codeSpans returns fenced blocks and inline spans, and no prose", () => {
  const text = [
    "Prose that mentions stow-s3 serve in a sentence.",
    "",
    "Inline `stow-s3 doctor --json` in a span.",
    "",
    "```bash",
    "stow-s3 workspace list",
    "```",
  ].join("\n");
  const spans = codeSpans(text);
  const joined = spans.join("\n");
  assert.ok(joined.includes("stow-s3 doctor --json"), "inline span missing");
  assert.ok(joined.includes("stow-s3 workspace list"), "fenced body missing");
  assert.ok(!joined.includes("in a sentence"), "prose leaked into the code spans");
});

test("prose is not evidence about a CLI", () => {
  // The three false positives the first version of this gate reported, all of
  // them real text in this repository.
  const prose = "Install the stow-s3 binary and run stow-s3 for a workspace.";
  assert.deepEqual(commandProblems(SURFACE, prose, "prose"), []);
  assert.deepEqual(flagProblems(SURFACE, prose, "prose"), []);
});

test("a path into a package directory is not an invocation", () => {
  const code = "node packages/stow-s3/scripts/benchmark-workspace.mjs --memory-only";
  assert.deepEqual(commandProblems(SURFACE, code, "path"), []);
  // And the flag is exempt rather than unknown, because the line is a real
  // documented command belonging to a Node script.
  assert.deepEqual(flagProblems(SURFACE, code, "path"), []);
});

test("a flag inside a shell script's own error message is not an invocation", () => {
  const code = 'STOW_BIN="${STOW_BIN:?set STOW_BIN to the stow-s3 binary}"';
  assert.deepEqual(invocations(code), []);
});

test("a renamed verb in a document is caught", () => {
  const problems = commandProblems(SURFACE, "stow-s3 workspace rename ws_1", "README.md");
  assert.equal(problems.length, 1);
  assert.ok(problems[0].includes("`stow-s3 workspace rename`"), problems[0]);
  assert.ok(problems[0].includes("names no workspace verb"), problems[0]);
});

test("a renamed subcommand in a document is caught", () => {
  const problems = commandProblems(SURFACE, "stow-s3 migrate --from v1", "README.md");
  assert.equal(problems.length, 1);
  assert.ok(problems[0].includes("`stow-s3 migrate`"), problems[0]);
  assert.ok(problems[0].includes("names no subcommand"), problems[0]);
});

test("a subcommand that exists is not reported", () => {
  assert.deepEqual(commandProblems(SURFACE, "stow-s3 prewarm --bucket b --keys k", "README.md"), []);
});

test("a flag stops the verb scan", () => {
  // `stow-s3 workspace --id ws_1` passes no verb, so there is nothing to check.
  // Reading the flag's value as a verb name would report every invocation that
  // carries an id, which is most of them.
  assert.deepEqual(commandProblems(SURFACE, "stow-s3 workspace --id ws_1", "README.md"), []);
  assert.deepEqual(commandProblems(SURFACE, "stow-s3 workspace --registry-dir /tmp/r list", "README.md"), []);
});

test("a removed flag in a document is caught", () => {
  const problems = flagProblems(SURFACE, "stow-s3 serve --port=0 --loglevel=debug", "README.md");
  assert.equal(problems.length, 1);
  assert.ok(problems[0].includes("`--loglevel`"), problems[0]);
});

test("the binary's own name is not reported as an unknown command", () => {
  // The first version sliced from the separator and read the token itself as the
  // argument, so every document reported `stow-s3 stow-s3`.
  assert.deepEqual(commandProblems(SURFACE, "stow-s3 serve", "README.md"), []);
  assert.deepEqual(commandProblems(SURFACE, "  stow-s3 workspace list", "README.md"), []);
  assert.deepEqual(commandProblems(SURFACE, "$(stow-s3 doctor)", "README.md"), []);
  assert.deepEqual(commandProblems(SURFACE, "stow-s3 serve && stow-s3 doctor", "README.md"), []);
});

test("a command with no arguments is not a problem", () => {
  assert.deepEqual(commandProblems(SURFACE, "stow-s3 doctor", "README.md"), []);
  assert.deepEqual(commandProblems(SURFACE, "stow-s3", "README.md"), []);
});

test("a flag is only judged on a line that runs the binary", () => {
  // A Node script's own flags in a block that never invokes stow-s3 are not
  // stow's flags to judge.
  assert.deepEqual(flagProblems(SURFACE, "npm ci --omit=optional", "README.md"), []);
});

test("an exemption is honoured", () => {
  const problems = commandProblems(SURFACE, "stow-s3 memory-only", "README.md");
  assert.deepEqual(problems, []);
});

test("an exemption for a token that has become real is reported", () => {
  // This is the half that stops the list growing forever. Without it an
  // exemption outlives its reason and starts hiding genuine problems.
  const surface = { ...SURFACE, commands: new Set(["serve", "doctor", "workspace", "prewarm", "memory-only"]) };
  const stale = staleExemptions(surface, NONE);
  assert.equal(stale.length, 1);
  assert.ok(stale[0].includes("memory-only"), stale[0]);
  assert.ok(stale[0].includes("benchmark-workspace.mjs"), "the reason should be reported");
});

test("an exemption that is still needed is not reported stale", () => {
  assert.deepEqual(staleExemptions(SURFACE, NONE), []);
});

test("every exemption carries a reason", () => {
  for (const [token, reason] of Object.entries(EXEMPT)) {
    assert.ok(token.length > 0, "empty exemption token");
    assert.ok(
      typeof reason === "string" && reason.length > 10,
      `exemption "${token}" has no reason; an unexplained exemption is a suppression`,
    );
  }
});

test("commandSurface refuses a surface it cannot trust", () => {
  // A gate that reads an empty surface reports every document as clean. The
  // guard is what stops a broken probe from turning the gate green forever.
  assert.throws(
    () => commandSurface("/nonexistent/stow-s3"),
    /could not read the command list|refusing to check/,
  );
});
