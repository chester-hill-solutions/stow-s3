#!/usr/bin/env node
// Checks that the commands and flags named in the documentation exist.
//
// The install-surface gate checks one thing about prose: whether it claims a
// package can be installed. It cannot see a document naming a workspace verb
// that was renamed, or a `--flag` a refactor removed, and that is a real failure
// mode for a project with fifteen CLI verbs and four documents. The verb table is
// already the single source of truth inside the binary — the usage line is
// derived from it rather than written out, for exactly this reason — and this
// gate asks the binary itself rather than reading its source.
//
// The surface comes from running the binary, not from parsing Go. An earlier
// version of this file read the `switch os.Args[1]` and the workspaceVerbs map
// with regular expressions, and every refactor that moved either would have
// silently emptied the surface and turned the gate into a green light.
// `workspace_contract_registry_test.go` already answers the same question for the
// contract's own flag table, by running `workspace --help` and each verb's `-h`;
// this is the same technique applied to the documents.
//
// Two rules, both scoped to code:
//
//  1. `stow-s3 <word>` in command position must name a subcommand or a verb.
//  2. A `--flag` in a code span that invokes the binary must be a registered flag.
//
// Scoping to code spans and to command position is what makes this usable. A
// first pass scanned whole lines and reported `stow-s3 for` from an English
// sentence, `--memory-only` from a path into a Node benchmark script, and
// `stow-s3 binary` from inside a shell script's error message. All three were
// real text and none was a command. Prose is not evidence about a CLI, and a
// gate that reports it is a gate that trains people to ignore it.
//
// The exemption list below is a ratchet, not an escape hatch: staleExemptions
// fails when a documented token becomes real while an exemption for it remains.
// That is what stops this file from accumulating "ignore this" lines forever.

import { readFileSync, existsSync, mkdtempSync, rmSync } from "node:fs";
import { spawnSync, execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";

const root = resolve(import.meta.dirname, "..");
const BINARY = "stow-s3";

// COMMAND_POSITION is where the binary can actually be the thing being run: the
// start of a line, or after something that ends a command. Requiring it is what
// separates a command from a word inside a quoted string.
const COMMAND_POSITION = /(?:^|[;&|`$(]\s*|\s(?:&&|\|\||then|do|else)\s+)$/;

// Tokens that appear in code spans and are not stow commands or flags. Each
// entry is a token and the reason it is exempt, so a reviewer can tell an
// exemption from a suppression.
export const EXEMPT = {
  // A Node script's own flag. The line names a path under packages/stow-s3 and
  // the flag belongs to that script, not to the binary.
  "memory-only": "benchmark-workspace.mjs flag, not a stow-s3 flag",
  // Third-party examples. The lines configure npm, not stow.
  "omit=optional": "npm flag, shown in an npm ci example",
  "pack-destination": "npm pack flag, shown in a packaging example",
};

// The documents this gate reads. A new one has to be added here or the gate
// cannot see it, which is the intended pressure.
export const DOCS = [
  "README.md",
  "CHANGELOG.md",
  "CONTRIBUTING.md",
  "SUPPORT.md",
  "site/agent.md",
  "site/llms.txt",
  "skills/stow-s3/SKILL.md",
  "docs/running-and-probing.md",
  "docs/task-manifest.md",
  "docs/workspace-contract.md",
  "docs/compat-contract.md",
  "docs/browser-persistence.md",
];

// run executes the binary and returns its combined output.
//
// spawnSync rather than execFileSync, and both streams rather than stdout, for a
// reason this gate's first version got wrong: the flag package prints usage to
// stderr and, for `-h`, exits 0. execFileSync therefore neither throws nor
// returns anything on stdout, and the probe silently found no flags at all —
// which would have left the gate green while checking six documents against an
// empty flag set. A probe that can return nothing has to be one that cannot fail
// quietly, and a caller that gets zero flags for a command with twenty of them
// should suspect the probe rather than the documentation.
function run(binary, args) {
  const result = spawnSync(binary, args, { encoding: "utf8" });
  if (result.error) return "";
  return `${result.stdout ?? ""}${result.stderr ?? ""}`;
}

// commandSurface asks the binary what it supports.
//
// Three probes, because the surface is assembled from three places: the top
// level prints its own commands in the usage text, `workspace --help` prints the
// verb list the dispatcher already derives from its table, and each verb's own
// `-h` prints the flags that verb registers. The union of the flag sets is used
// rather than a per-verb map, because a document naming `--registry-dir` is
// correct whether the verb it documents is `resume` or `prune`, and a
// per-verb rule would report a correct document as wrong on the strength of
// which line the flag appeared on.
export function commandSurface(binary) {
  const commands = new Set();
  const verbs = new Set();
  const flags = new Set();

  const topLevel = run(binary, ["--help"]);
  const usage = topLevel.match(/commands:\n([\s\S]*?)(?:\n\n|$)/);
  if (!usage) throw new Error(`could not read the command list from ${BINARY} --help`);
  for (const match of usage[1].matchAll(/^\s{2}([a-z][a-z0-9-]*)\s{2,}/gm)) commands.add(match[1]);

  const workspaceHelp = run(binary, ["workspace", "--help"]);
  const verbLine = workspaceHelp.match(/usage: stow-s3 workspace <([^>]+)>/);
  if (!verbLine) throw new Error("could not read the workspace verb list from the binary");
  for (const verb of verbLine[1].split("|")) verbs.add(verb.trim());

  // The flag set of every subcommand and every verb. `-h` is how the flag
  // package prints one. Probing only the verbs was a bug the first run against
  // the real documents caught: `stow-s3 -h` prints the top-level usage, which
  // carries no flags at all, so `--mode`, `--port`, `--data-dir`, `--bucket`,
  // `--keys` and `--offline` were all reported as unregistered.
  const probes = [["-h"]];
  for (const command of commands) probes.push([command, "-h"]);
  for (const verb of verbs) probes.push(["workspace", verb, "-h"]);
  for (const args of probes) {
    for (const match of run(binary, args).matchAll(/^\s{2}-([a-z][a-z0-9-]*)/gm)) flags.add(match[1]);
  }
  // Help is handled by the flag package rather than registered, and both
  // spellings work.
  flags.add("help");
  flags.add("h");

  // A gate that reads an empty surface reports every document as clean, and that
  // is the failure mode worth being paranoid about: the first version of this
  // probe read stdout only, found no flags anywhere, and reported all ten
  // documents as correct while checking them against nothing. These thresholds
  // turn that class of bug into a loud failure.
  //
  // The numbers are floors, not exact counts. This command registers far more
  // than ten flags, so a probe finding fewer is broken rather than describing a
  // smaller CLI.
  if (commands.size < 3) {
    throw new Error(`the binary reported ${commands.size} subcommands; refusing to check against it`);
  }
  if (flags.size < 10) {
    throw new Error(
      `the binary reported ${flags.size} flags across ${commands.size} subcommands and ` +
        `${verbs.size} verbs. A probe that finds almost no flags has failed, not found a ` +
        `smaller CLI: the usage text goes to stderr and the flag package exits 0 for -h.`,
    );
  }
  return { commands, verbs, flags };
}

// buildBinary compiles the command so the gate does not depend on a binary that
// happens to be lying around, which would be the same class of problem as a
// baseline edited without review.
export function buildBinary() {
  const dir = mkdtempSync(join(tmpdir(), "stow-docgate-"));
  const out = join(dir, BINARY);
  execFileSync("go", ["build", "-o", out, "./cmd/stow-s3"], { cwd: root, stdio: ["ignore", "pipe", "pipe"] });
  return { path: out, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

// codeSpans returns the code in a document and nothing else: every fenced block
// body and every inline `span`. Prose is excluded by construction, which is the
// whole reason the rules produce no false positives.
export function codeSpans(text) {
  const spans = [];
  for (const fence of text.matchAll(/```[^\n]*\n([\s\S]*?)```/g)) {
    for (const line of fence[1].split("\n")) spans.push(line);
  }
  const withoutFences = text.replace(/```[\s\S]*?```/g, "");
  for (const span of withoutFences.matchAll(/`([^`\n]+)`/g)) {
    for (const line of span[1].split("\n")) spans.push(line);
  }
  return spans;
}

// invocations finds the places a code span runs the binary, returning the text
// after the token so a caller can read the arguments. A path segment such as
// packages/stow-s3/scripts/x.mjs is not an invocation.
export function invocations(code) {
  const found = [];
  let from = 0;
  for (;;) {
    const at = code.indexOf(BINARY, from);
    if (at === -1) break;
    from = at + BINARY.length;
    const after = code[from];
    if (after !== undefined && /[a-z0-9/-]/.test(after)) continue;
    const before = code.slice(0, at);
    if (at > 0 && /[a-z0-9/-]/.test(before[before.length - 1])) continue;
    if (!COMMAND_POSITION.test(before)) continue;
    found.push({ start: at, args: code.slice(from) });
  }
  return found;
}

// argumentsFor reads the command words a code span passes to the binary, stopping
// at the first flag.
//
// A workspace verb is the second word when the first is `workspace`, and that
// second word is the one a rename breaks. Checking only the first word meant
// `stow-s3 workspace rename` passed, because `workspace` is a valid command —
// so the gate's main subject, the verb list, was never the thing being checked.
// The unit test for a renamed verb is what caught it.
export function argumentsFor(args) {
  const words = [];
  let rest = args.replace(/^\s+/, "");
  for (let i = 0; i < 2; i += 1) {
    const match = rest.match(/^([a-z][a-z0-9-]*)\s*/);
    if (!match) break;
    words.push(match[1]);
    rest = rest.slice(match[0].length);
    // A flag ends the command: `stow-s3 workspace --id ws_1` names no verb.
    if (rest.startsWith("-")) break;
  }
  return words;
}

export function commandProblems(surface, code, location, exempt = EXEMPT) {
  const problems = [];
  for (const { args } of invocations(code)) {
    const [first, second] = argumentsFor(args);
    if (first === undefined) continue;
    if (first in exempt) continue;
    if (!surface.commands.has(first)) {
      problems.push(`${location}: \`${BINARY} ${first}\` names no subcommand`);
      continue;
    }
    // `workspace` is the one command that takes a verb, and only then.
    if (first !== "workspace" || second === undefined) continue;
    if (second in exempt) continue;
    if (surface.verbs.has(second) || second === "help") continue;
    problems.push(`${location}: \`${BINARY} ${first} ${second}\` names no workspace verb`);
  }
  return problems;
}

export function flagProblems(surface, code, location, exempt = EXEMPT) {
  const problems = [];
  if (invocations(code).length === 0) return problems;
  for (const match of code.matchAll(/--([a-z][a-z0-9-]*)/g)) {
    const name = match[1];
    if (surface.flags.has(name)) continue;
    if (name in exempt) continue;
    problems.push(`${location}: \`--${name}\` is not a flag ${BINARY} registers`);
  }
  return problems;
}

// staleExemptions is the other half of the exemption list: a token that has
// become real while an exemption for it remains means the exemption is now
// hiding a genuine problem instead of documenting a real one.
export function staleExemptions(surface, docs, exempt = EXEMPT) {
  const stale = [];
  const known = new Set([...surface.commands, ...surface.verbs, ...surface.flags]);
  for (const token of Object.keys(exempt)) {
    if (!known.has(token)) continue;
    stale.push(`exemption "${token}" is now a real command or flag; remove it (${exempt[token]})`);
  }
  void docs;
  return stale;
}

export function checkScriptProblems(surface, docs, exempt = EXEMPT) {
  const problems = [];
  for (const [path, text] of Object.entries(docs)) {
    for (const [index, line] of codeSpans(text).entries()) {
      const location = `${path} (code block ${index + 1})`;
      problems.push(...commandProblems(surface, line, location, exempt));
      problems.push(...flagProblems(surface, line, location, exempt));
    }
  }
  problems.push(...staleExemptions(surface, docs, exempt));
  return problems;
}

function main() {
  const built = buildBinary();
  let surface;
  try {
    surface = commandSurface(built.path);
  } finally {
    built.cleanup();
  }

  const docs = {};
  const missing = [];
  for (const path of DOCS) {
    const full = join(root, path);
    if (!existsSync(full)) {
      missing.push(path);
      continue;
    }
    docs[path] = readFileSync(full, "utf8");
  }

  const problems = checkScriptProblems(surface, docs);
  for (const path of missing) problems.push(`declared document is missing: ${path}`);

  if (problems.length > 0) {
    console.error("Documentation names commands or flags that do not exist:");
    for (const problem of problems) console.error(`  ${problem}`);
    process.exit(1);
  }
  console.log(
    `Doc command surface OK (${surface.commands.size} commands, ${surface.verbs.size} verbs, ` +
      `${surface.flags.size} flags, ${Object.keys(docs).length} documents)`,
  );
}

if (import.meta.filename === process.argv[1]) main();
