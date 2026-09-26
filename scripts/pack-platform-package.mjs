#!/usr/bin/env node
// Pack one native platform package from a release archive.
//
//   node scripts/pack-platform-package.mjs <archive.tar.gz> [out-dir]
//
// The archive is named stow-<goos>-<goarch>.tar.gz by the release build. The
// npm package name uses npm's arch spelling, which differs for amd64/x64, so
// the translation lives here rather than in the workflow: a mismatch would
// otherwise fail only at release time.
import { chmodSync, existsSync, mkdirSync, mkdtempSync, copyFileSync, readFileSync, rmSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import { basename, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(import.meta.dirname, "..");
const NPM_ARCH = { amd64: "x64", arm64: "arm64" };

// platformFor derives the npm platform package from a release archive name.
//
// It is separated from the packing so the mapping can be tested. The archive name
// is the only thing that says which platform a tarball is for, so a pattern that
// stops matching produces no package rather than a wrong one - and "no package" is
// only noticed by a later step in a release, which is too late to be useful.
export function platformFor(archiveName) {
  const match = basename(archiveName).match(/^stow-([a-z0-9]+)-([a-z0-9]+)\.tar\.gz$/);
  if (!match) {
    return { error: `archive name is not stow-<goos>-<goarch>.tar.gz: ${basename(archiveName)}` };
  }
  const [, goos, goarch] = match;
  const npmArch = NPM_ARCH[goarch];
  if (npmArch === undefined) {
    return { error: `no npm platform package defined for ${goos}/${goarch}` };
  }
  return { goos, goarch, npmArch, platformDir: `stow-s3-${goos}-${npmArch}` };
}

export function main(argv) {
  const [archiveArg, outDirArg] = argv.slice(2);
  if (!archiveArg) {
    console.error("usage: pack-platform-package.mjs <archive.tar.gz> [out-dir]");
    return 64;
  }

  const archive = resolve(archiveArg);
  const outDir = resolve(root, outDirArg ?? "dist/npm-platform");
  const platform = platformFor(archive);
  if (platform.error) {
    console.error(platform.error);
    return 1;
  }
  const { goos, goarch, npmArch, platformDir } = platform;

  const packageDir = resolve(root, "packages", platformDir);
  const manifestPath = resolve(packageDir, "package.json");
  if (!existsSync(manifestPath)) {
    console.error(`missing platform package: packages/${platformDir}/package.json`);
    return 1;
  }
  if (!existsSync(archive)) {
    console.error(`archive not found: ${archive}`);
    return 1;
  }

  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  // The manifest is what npm resolves by os and cpu, so a package packed for one
  // platform and declaring another installs and then fails to run.
  if (manifest.os?.[0] !== goos || manifest.cpu?.[0] !== npmArch) {
    console.error(
      `${manifest.name} declares os=${JSON.stringify(manifest.os)} cpu=${JSON.stringify(manifest.cpu)}, ` +
        `but was packed from ${goos}/${npmArch}`,
    );
    return 1;
  }

  // Extract the binary, stage it into the package, and pack. The staged copy lives
  // in a gitignored path, so a committed manifest never claims to contain a
  // binary that is not there.
  const work = mkdtempSync(join(tmpdir(), "stow-platform-"));
  try {
    const extracted = join(work, "stow-s3");
    execFileSync("tar", ["-xzf", archive, "-C", work, "stow-s3"], { stdio: "inherit" });
    const staged = join(packageDir, "bin", "stow-s3");
    mkdirSync(join(packageDir, "bin"), { recursive: true });
    copyFileSync(extracted, staged);
    chmodSync(staged, 0o755);

    mkdirSync(outDir, { recursive: true });
    const packed = execFileSync(
      "npm",
      ["pack", "--ignore-scripts", "--json", "--pack-destination", outDir],
      { cwd: packageDir, encoding: "utf8" },
    );
    const [{ filename, size, files }] = JSON.parse(packed);
    // A tarball without the binary installs and fails at first run, which is a
    // worse failure than refusing to pack it.
    if (!files.map((file) => file.path).includes("bin/stow-s3")) {
      console.error(`${manifest.name} packed without bin/stow-s3; refusing to publish`);
      return 1;
    }
    console.log(
      `${manifest.name} (${goos}/${goarch}): ${filename} (${(size / 1048576).toFixed(1)} MB)`,
    );
    return 0;
  } finally {
    rmSync(work, { recursive: true, force: true });
    rmSync(resolve(packageDir, "bin"), { recursive: true, force: true });
  }
}

const invokedDirectly =
  process.argv[1] !== undefined && resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (invokedDirectly) {
  process.exit(main(process.argv));
}
