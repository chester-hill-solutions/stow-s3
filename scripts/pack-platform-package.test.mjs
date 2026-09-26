import assert from "node:assert/strict";
import { test } from "node:test";

import { platformFor } from "./pack-platform-package.mjs";

// The archive name is the only thing that says which platform a tarball is for.
// npm spells the architecture differently from Go - amd64 is x64 - so this
// translation is where a package gets built for the wrong platform, and the only
// other check on it runs during a release.

test("amd64 becomes npm's x64", () => {
  const platform = platformFor("stow-linux-amd64.tar.gz");
  assert.equal(platform.goos, "linux");
  assert.equal(platform.goarch, "amd64");
  assert.equal(platform.npmArch, "x64");
  assert.equal(platform.platformDir, "stow-s3-linux-x64");
});

test("arm64 is spelled the same on both sides", () => {
  const platform = platformFor("stow-darwin-arm64.tar.gz");
  assert.equal(platform.npmArch, "arm64");
  assert.equal(platform.platformDir, "stow-s3-darwin-arm64");
});

// All four the release build produces, so adding a target cannot quietly produce a
// directory name no package claims.
test("every released platform maps to a package directory", () => {
  const expected = [
    ["stow-linux-amd64.tar.gz", "stow-s3-linux-x64"],
    ["stow-linux-arm64.tar.gz", "stow-s3-linux-arm64"],
    ["stow-darwin-amd64.tar.gz", "stow-s3-darwin-x64"],
    ["stow-darwin-arm64.tar.gz", "stow-s3-darwin-arm64"],
  ];
  for (const [archive, dir] of expected) {
    assert.equal(platformFor(archive).platformDir, dir, `${archive} mapped wrong`);
  }
});

test("an unknown architecture is refused rather than guessed", () => {
  const platform = platformFor("stow-linux-riscv64.tar.gz");
  assert.match(platform.error, /no npm platform package defined for linux\/riscv64/);
  assert.equal(platform.platformDir, undefined);
});

// A name that does not match produces no package. That is the safe direction: the
// alternative is a directory built from whatever the pattern happened to capture.
test("a name that does not match is refused", () => {
  for (const name of [
    "stow-linux-amd64.tar",
    "stow-linux-amd64.zip",
    "stow-Linux-amd64.tar.gz",
    "linux-amd64.tar.gz",
    "stow-linux-amd64-extra.tar.gz",
    "",
  ]) {
    assert.ok(platformFor(name).error, `${name} should not resolve to a platform`);
  }
});

// The pattern is anchored and the capture is [a-z0-9]+, so a name with a path
// separator or a dot in the captured part cannot smuggle a different directory out.
test("a traversing name cannot escape the packages directory", () => {
  const platform = platformFor("stow-..-..-amd64.tar.gz");
  assert.ok(platform.error || platform.platformDir.startsWith("stow-s3-"));
});

// The spelling difference is the whole reason this function exists, so it is
// asserted directly rather than only through the directory name.
test("the go and npm architecture names are not assumed identical", () => {
  assert.notEqual(platformFor("stow-linux-amd64.tar.gz").goarch, platformFor("stow-linux-amd64.tar.gz").npmArch);
});
