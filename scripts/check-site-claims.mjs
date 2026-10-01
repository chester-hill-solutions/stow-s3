#!/usr/bin/env node
// The site is the one surface here that a human reads before any of the gated
// documents, and for years nothing checked it. Three of its claims were wrong
// while every gate stayed green:
//
//   - the Go install line was `go get /pkg/stow`, which is not a module path
//   - the Go snippet used `Backend: memory`, which does not compile; the
//     constant is stow.BackendMemory
//   - the social card told people to `npm install @chs/stow-s3`, which is not
//     a published package name
//
// All three were on the page and in the card a reader sees first, and all three
// were invisible to check-install-surface, which covers README.md, this
// directory's agent.md, llms.txt and the skill - and not index.html. The two
// documents an agent reads were gated; the one a person lands on was not.
//
// What this file is NOT is a design review. A page can be beautifully written and
// still describe a product that no longer exists, and only a person catches that;
// the stale positioning this was written alongside cannot be machine-checked. What
// this can catch is a page that tells a reader to run something that does not
// work, or that quotes a number the build no longer produces.
//
// The decision is siteClaimProblems, a pure function of the site's text and the
// build's sizes, so it can be tested against fixtures rather than only by running
// it against the tree. Every check reads the site with markup stripped: matching
// raw HTML against a command is a gate that starts reporting failures the moment
// someone wraps half the command in a <b>, which is how a gate learns to be
// ignored.

import { readFileSync, existsSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

export const SITE_TEXT_FILES = [
  "site/index.html",
  "site/agent.md",
  "site/llms.txt",
  ...["01-hero", "02-problem", "03-session", "04-agents", "05-ecosystem", "og"].map(
    (card) => `site/launch/${card}.html`,
  ),
];

// Each command the site must be able to print, checked across the site rather than
// per file. agent.md deliberately tells you to build from a checkout rather than to
// install the published package, so demanding the npm line there would be demanding
// something it is right not to say. check-install-surface already owns the
// per-document rule; this owns the site-wide one.
export const REQUIRED_COMMANDS = [
  "npm install @chester-hill-solutions/stow-s3",
  "go get github.com/chester-hill-solutions/stow-s3/pkg/stow",
];

// Short forms that must never appear anywhere. A bare `/pkg/stow` is the one that
// shipped: it reads like a path fragment of the real module path, so it looks right
// in a code listing and resolves to nothing.
export const FORBIDDEN = [
  { needle: "go get /pkg/stow", why: "not a module path; the full path is required" },
  { needle: "go get pkg/stow", why: "not a module path; the full path is required" },
  { needle: "Backend: memory", why: "does not compile; the constant is stow.BackendMemory" },
  { needle: "@chs/stow", why: "not a published package name" },
];

// The quota claims, which are two literals that have to agree. The page said
// 16 MiB / 1,000 objects while internal/runtime/types.go said 64 MiB / 10,000.
export const QUOTAS = [
  { stated: "64 MiB", superseded: "16 MiB", source: "DefaultMaxBytes in internal/runtime/types.go" },
  { stated: "10,000 objects", superseded: "1,000 objects", source: "DefaultMaxObjects in internal/runtime/types.go" },
];

const WASM = "packages/stow-s3/dist/stow-runtime.wasm";

// The size claims, each naming where its number comes from. A number that drifts
// silently is worse than no number, because it reads as a measurement.
export const SIZE_CLAIMS = [
  { file: "site/index.html", pattern: /(\d+(?:\.\d+)?) MB\s*single binary/, build: "bin/stow-s3" },
  { file: "site/index.html", pattern: /(\d+(?:\.\d+)?) MB (?:runtime|WASM runtime)/, build: WASM },
  { file: "site/launch/05-ecosystem.html", pattern: /(\d+(?:\.\d+)?) MB (?:runtime|WASM runtime)/, build: WASM },
];

// Tags become a single space rather than nothing, so `go get</b> <b>pkg/stow`
// reads as two words and is not mistaken for one contiguous command. Whitespace is
// collapsed across newlines too, and not only spaces: the Go install line in
// index.html is wrapped across a newline, so a check that collapsed only spaces saw
// `go get\n /pkg/stow` and reported nothing. That was not hypothetical - it is what
// happened the first time this was deliberately broken.
export const asText = (markup) =>
  markup
    .replace(/<[^>]+>/g, " ")
    .replace(/&mdash;|&ndash;/g, "—")
    .replace(/&middot;/g, "·")
    .replace(/&hellip;/g, "…")
    .replace(/&amp;/g, "&")
    .replace(/&quot;/g, '"')
    .replace(/&#(\d+);/g, (_, code) => String.fromCharCode(Number(code)))
    .replace(/\s+/g, " ");

// siteClaimProblems is the whole gate. `text` maps a site file to its markup-stripped
// text; `sizes` maps a build path to its size in megabytes. A build that was not
// present is simply absent from `sizes` and its claim is not checked, rather than
// being reported as a failure - a gate that fails because a build was skipped is a
// gate that trains people to skip builds.
export function siteClaimProblems({ text, sizes }) {
  const problems = [];
  const everyFile = [...text.keys()];

  for (const command of REQUIRED_COMMANDS) {
    if (![...text.values()].some((body) => body.includes(command))) {
      problems.push(`no site file prints the install command ${command}`);
    }
  }

  for (const file of everyFile) {
    for (const { needle, why } of FORBIDDEN) {
      if (text.get(file).includes(needle)) {
        problems.push(`${file} prints "${needle}": ${why}`);
      }
    }
  }

  for (const claim of SIZE_CLAIMS) {
    const body = text.get(claim.file);
    const actual = sizes.get(claim.build);
    if (body === undefined || actual === undefined) continue;
    const match = body.match(claim.pattern);
    if (!match) continue;
    const claimed = Number(match[1]);
    if (Math.abs(claimed - actual) > 0.1) {
      problems.push(`${claim.file} claims ${claimed} MB where ${claim.build} is ${actual.toFixed(1)} MB`);
    }
  }

  const index = text.get("site/index.html");
  if (index !== undefined) {
    for (const quota of QUOTAS) {
      if (index.includes(quota.superseded)) {
        problems.push(`site/index.html states the superseded limit ${quota.superseded}; the source is ${quota.source}`);
      }
      if (!index.includes(quota.stated)) {
        problems.push(`site/index.html does not state ${quota.stated}; the source is ${quota.source}`);
      }
    }
  }

  return problems;
}

// run applies the gate to the working tree. Kept separate from the decision so the
// decision can be tested without a checkout, which is the same split the other
// checks in this directory use.
export function run({ repoRoot }) {
  const text = new Map();
  for (const file of SITE_TEXT_FILES) {
    const path = join(repoRoot, file);
    if (!existsSync(path)) continue;
    text.set(file, asText(readFileSync(path, "utf8")));
  }
  const sizes = new Map();
  for (const build of new Set(SIZE_CLAIMS.map((claim) => claim.build))) {
    const path = join(repoRoot, build);
    if (!existsSync(path)) continue;
    sizes.set(build, statSync(path).size / 1048576);
  }
  return siteClaimProblems({ text, sizes });
}

if (process.argv[1] && import.meta.url.endsWith(process.argv[1].split("/").pop())) {
  const repoRoot = resolve(import.meta.dirname, "..");
  const problems = run({ repoRoot });
  if (problems.length) {
    console.error("Site claims do not match the tree");
    for (const problem of problems) console.error(`  ${problem}`);
    console.error("Fix the page, or the build. Do not relax the claim to match a stale build.");
    process.exit(1);
  }
  console.log(
    `Site claims OK (${REQUIRED_COMMANDS.length} install commands, ${FORBIDDEN.length} forbidden forms ` +
      `across ${SITE_TEXT_FILES.length} files, ${SIZE_CLAIMS.length} size claims, ${QUOTAS.length} quota claims)`,
  );
}
