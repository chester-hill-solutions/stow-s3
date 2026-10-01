// The site's claims, tested against fixtures rather than only by running the gate
// against the tree.
//
// The reason this file exists is a specific failure. Three claims on the site were
// wrong while every other gate stayed green: a Go install line that was not a
// module path, a Go snippet that did not compile, and a social card naming a
// package that does not exist. None of them was caught, because the site was not
// checked. So the gate was added - and the first version of it passed a
// deliberately reintroduced `go get /pkg/stow`, because prettier wraps that line
// across a newline and the check collapsed only spaces.
//
// That is the whole argument for testing the decision rather than the script: the
// break that mattered was invisible to the running check, and a break that is
// invisible to the running check is a gate that is green for the wrong reason.
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { join, resolve } from "node:path";
import { test } from "node:test";

import { asText, siteClaimProblems, QUOTAS, RELEASE_TARGETS, SIZE_CLAIMS } from "./check-site-claims.mjs";

const WASM = "packages/stow-s3/dist/stow-runtime.wasm";

// A site that satisfies every rule, so each test can break exactly one thing and
// the failure it sees is the failure it caused.
const cleanIndex = `
  <h1>Working storage an agent can hand off.</h1>
  <ul class="proof">
    <li><b>12.7 MB max</b><span>single binary, every platform</span></li>
    <li><b>5.8 MB</b><span>runtime</span></li>
  </ul>
  <p>Defaults are <strong>64 MiB</strong> and <strong>10,000 objects</strong>.</p>
  <code id="install-cmd">npm install @chester-hill-solutions/stow-s3</code>
  <span class="installline"><b>go get</b>
    github.com/chester-hill-solutions/stow-s3/pkg/stow</span>
  <pre>rt, _ := stow.Open(stow.Options{
    Backend: stow.BackendMemory,
  })</pre>
`;

const cleanEcosystem = `<span class="installline"><b>5.8 MB</b> runtime</span>`;

const sizes = new Map([
  ["bin/stow-s3", 12.66],
  ["packages/stow-s3/dist/stow-runtime.wasm", 5.83],
]);

const site = (index = cleanIndex, ecosystem = cleanEcosystem) =>
  new Map([
    ["site/index.html", asText(index)],
    ["site/launch/05-ecosystem.html", asText(ecosystem)],
  ]);

test("a site that states everything correctly has no problems", () => {
  assert.deepEqual(siteClaimProblems({ text: site(), sizes }), []);
});

test("markup between the halves of a command does not hide it", () => {
  // The shape the page actually has: the command is split across elements and a
  // newline. A check that matched raw HTML would report a false failure here, and
  // a check that collapsed only spaces would miss a real one.
  const problems = siteClaimProblems({ text: site(), sizes });
  assert.equal(problems.length, 0, JSON.stringify(problems));
});

test("a Go install line that is not a module path is reported", () => {
  const broken = cleanIndex.replace(
    "github.com/chester-hill-solutions/stow-s3/pkg/stow",
    "/pkg/stow",
  );
  const problems = siteClaimProblems({ text: site(broken), sizes });
  // Two, and both are right: the short form is forbidden, and the real command it
  // replaced is now absent. Reporting only the first would let a page drop the
  // install line entirely and pass.
  assert.ok(
    problems.some((p) => /go get \/pkg\/stow/.test(p) && /not a module path/.test(p)),
    JSON.stringify(problems),
  );
  assert.ok(
    problems.some((p) => /no site file prints the install command go get/.test(p)),
    JSON.stringify(problems),
  );
});

test("the short form is reported even when markup and a newline split it", () => {
  // This is the break the first version of the gate missed. It has its own case
  // because the fix was to collapse all whitespace, and an edit to that collapse
  // would otherwise leave the wrapping test above passing while this one silently
  // stopped detecting anything.
  const broken = `<span class="installline"><b>go get</b>
                /pkg/stow</span>`;
  const text = new Map([["site/index.html", asText(broken)]]);
  const problems = siteClaimProblems({ text, sizes });
  assert.ok(
    problems.some((p) => /go get \/pkg\/stow/.test(p)),
    `the split short form went unreported; problems were ${JSON.stringify(problems)}`,
  );
});

test("a required command split across a newline is still found", () => {
  // The mirror of the case above, and the reason the collapse is not one-sided: a
  // check that stops recognising a command once it is wrapped would start
  // demanding a command it can no longer see.
  const wrapped = asText(
    `<code id="install-cmd">npm install
       @chester-hill-solutions/stow-s3</code>`,
  );
  assert.ok(wrapped.includes("npm install @chester-hill-solutions/stow-s3"));
});

test("a Go snippet that does not compile is reported", () => {
  const broken = cleanIndex.replace("stow.BackendMemory", "memory");
  const problems = siteClaimProblems({ text: site(broken), sizes });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /does not compile/);
});

test("a package name that is not published is reported, on any site file", () => {
  const text = site();
  text.set("site/launch/og.html", asText("<span>npm install @chs/stow-s3</span>"));
  const problems = siteClaimProblems({ text, sizes });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /og\.html prints "@chs\/stow"/);
});

test("a size claim is checked against the build, not trusted", () => {
  const broken = cleanIndex.replace("12.7 MB max", "10.6 MB max");
  const problems = siteClaimProblems({ text: site(broken), sizes });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /claims 10\.6 MB where bin\/stow-s3 is 12\.7 MB/);
});

test("a build that was not produced is not a failure", () => {
  // A gate that fails because a build was skipped is a gate that trains people to
  // skip builds, so an absent build means the claim is unchecked, not wrong.
  const problems = siteClaimProblems({ text: site(), sizes: new Map() });
  assert.deepEqual(problems, [], JSON.stringify(problems));
});

test("a size claim is skipped rather than failed when the build is absent", () => {
  // And specifically: a claim that is merely absent from the page is not an error
  // either, because the card that carries it may not exist on every site.
  const text = new Map([["site/index.html", asText("<p>no numbers here</p>")]]);
  const problems = siteClaimProblems({ text, sizes });
  assert.deepEqual(problems.filter((p) => /MB/.test(p)), []);
});

test("the superseded quota is reported and the current one demanded", () => {
  const broken = cleanIndex.replace("64 MiB", "16 MiB").replace("10,000 objects", "1,000 objects");
  const problems = siteClaimProblems({ text: site(broken), sizes });
  assert.equal(problems.length, QUOTAS.length * 2);
  assert.ok(problems.some((p) => /superseded limit 16 MiB/.test(p)));
  assert.ok(problems.some((p) => /does not state 64 MiB/.test(p)));
});

test("an install command missing from every file is reported once", () => {
  const text = site(cleanIndex.replace(/npm install @chester-hill-solutions\/stow-s3/, "install it"));
  const problems = siteClaimProblems({ text, sizes });
  assert.equal(problems.filter((p) => /no site file prints/.test(p)).length, 1);
});

test("every size claim names a build path that exists in the tree", () => {
  // A claim whose build path were a typo would never be checked, and the gate would
  // report itself green while measuring nothing. That is the risk the absent-build
  // guard exists for, and it is not observable from siteClaimProblems alone: a
  // missing build is skipped either way, and `Math.abs(x - undefined)` is NaN, which
  // compares false against every threshold. So the path is checked here instead,
  // against the real tree.
  //
  // Two of the three are produced by a build rather than committed, so their absence
  // is expected and only a path that is neither present nor a known build output is
  // a problem.
  const repoRoot = resolve(import.meta.dirname, "..");
  const produced = new Set([WASM, "bin/stow-s3"]);
  for (const claim of SIZE_CLAIMS) {
    assert.ok(claim.build, `a size claim for ${claim.file} names no build`);
    const exists = existsSync(join(repoRoot, claim.build));
    assert.ok(
      exists || produced.has(claim.build),
      `${claim.file} measures ${claim.build}, which is neither present nor a known build output`,
    );
  }
});

// Dropping the word "max" from the page used to switch the claim off: the pattern
// required it, a pattern that does not match is skipped, and a per-platform number
// went back in unchecked. The gate was green while checking nothing.
test("editing the page's wording cannot stop the size claim being checked", () => {
  const reworded = cleanIndex.replace("12.7 MB max", "11.7 MB");
  const problems = siteClaimProblems({ text: site(reworded), sizes });
  assert.equal(problems.length, 1, JSON.stringify(problems));
  assert.match(problems[0], /claims 11\.7 MB/);
});

// The measurement was platform-dependent, which is why it claimed 11.7 MB while a
// linux/amd64 user downloaded 12.4 MB. Narrowing the targets to the one platform the
// claim was written on would restore exactly that defect silently.
test("the size claim is measured across every published platform", () => {
  assert.equal(RELEASE_TARGETS.length, 4);
  assert.deepEqual([...new Set(RELEASE_TARGETS.map((t) => t.split("/")[0]))].sort(), ["darwin", "linux"]);
  for (const target of RELEASE_TARGETS) {
    assert.match(target, /^(linux|darwin)\/(amd64|arm64)$/, `${target} is not a release target`);
  }
});
