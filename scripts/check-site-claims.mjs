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

import { readFileSync, existsSync, statSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

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
// Both lines are required. A scoped package on GitHub Packages is routed by the scope
// binding, so `npm install @chester-hill-solutions/stow-s3` on its own resolves against
// npmjs and finds nothing - and the gate used to require exactly that string while the
// page shipped it as the headline install, so it was enforcing a command that does not
// work. Requiring the binding makes the two inseparable again.
export const SCOPE_REGISTRY = "npm config set @chester-hill-solutions:registry https://npm.pkg.github.com";

export const REQUIRED_COMMANDS = [
  "npm install @chester-hill-solutions/stow-s3",
  SCOPE_REGISTRY,
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
// The four platforms a release publishes. The native binary's size differs by a
// megabyte across them, so a claim measured from whichever one the gate happened to
// run is a claim about the machine, not about the project: "11.7 MB" was true only
// for darwin/arm64, the platform the claim was written on, while a linux/amd64 user
// downloaded 12.4 MB. The page therefore states the largest, and the gate measures
// the largest, which is the only reading that cannot be false for somebody.
export const RELEASE_TARGETS = ["linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"];

export const SIZE_CLAIMS = [
  // "max" is optional in the pattern on purpose. Required, it turns editing the page's
  // wording into a way to stop the gate checking the page: dropping the word stopped the
  // pattern matching, and a non-matching claim is skipped, so a per-platform number went
  // back in unchecked. Matching both shapes means the number is always compared; the page
  // still has to be honest about which number it is.
  { file: "site/index.html", pattern: /(\d+(?:\.\d+)?) MB(?: max)?\s*single binary/, build: "bin/stow-s3" },
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

// largestPublishedBinary cross-compiles every platform a release publishes and
// returns the biggest one in megabytes, or undefined when the tree cannot be built
// (no toolchain, or a target this machine cannot reach). Undefined rather than zero,
// because an unmeasured claim is skipped by siteClaimProblems and a zero would be a
// claim that the binary is empty.
export function largestPublishedBinary(repoRoot) {
  const dir = mkdtempSync(join(tmpdir(), "stow-sizes-"));
  try {
    let largest = 0;
    let measured = 0;
    for (const target of RELEASE_TARGETS) {
      const slash = target.indexOf("/");
      const env = { ...process.env, GOOS: target.slice(0, slash), GOARCH: target.slice(slash + 1) };
      const out = join(dir, target.replace("/", "-"));
      // Two argv entries, not one: there is no shell here to split "-s -w" out of
      // its quotes, and go rejects a flag value that still carries them.
      const built = spawnSync("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", out, "./cmd/stow-s3"], {
        cwd: repoRoot, env, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"],
      });
      if (built.status !== 0 || !existsSync(out)) continue;
      largest = Math.max(largest, statSync(out).size / 1048576);
      measured += 1;
    }
    // Fewer than every target means a partial measurement, and the largest of a
    // subset understates the whole. Reporting that as a pass is the failure this
    // gate exists to prevent.
    if (measured < RELEASE_TARGETS.length) {
      throw new Error(
        `measured ${measured} of ${RELEASE_TARGETS.length} release targets; the largest binary would be a claim about a subset, which is how this gate printed "Site claims OK" having measured none of them`,
      );
    }
    return largest;
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

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
    // Both sides rounded to one decimal, because one is stated on the page and the
    // other is a measurement of 13 million bytes. Comparing a page's number against a
    // full-precision one made the boundary depend on which side of .05 the build fell.
    if (Math.abs(claimed - Number(actual.toFixed(1))) > 0.1) {
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
    if (build === "bin/stow-s3") {
      // Measured by cross-compiling every published target and keeping the largest,
      // so the answer does not depend on which machine ran the gate.
      sizes.set(build, largestPublishedBinary(repoRoot));
      continue;
    }
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
