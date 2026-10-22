#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

// versionProblems compares the versions this repository declares in four places
// and reports every disagreement.
//
// It is a pure function of the discovered strings, separated from the reading, so
// it can be tested. The risk it exists to cover is a source pattern that stops
// matching: every version here is found by a regex or a JSON field, and a pattern
// that no longer matches yields undefined. Treating that as agreement would make
// the gate pass because it could not see.
export function versionProblems({ goVersion, packageVersion, pythonProjectVersion, pythonInitVersion }) {
  const problems = [];
  if (!goVersion) {
    // The Go constant is the anchor every other version is compared against, so
    // once it cannot be read the comparisons against it are not information - they
    // are the same unreadable value failing four times. The Python pair is
    // independent of the anchor and is still checked.
    problems.push("internal/version/version.go declares no version");
  } else {
    if (goVersion !== packageVersion) {
      problems.push(`version mismatch: internal/version=${goVersion}, packages/stow-s3=${packageVersion}`);
    }
    if (!pythonProjectVersion) {
      problems.push("packages/stow-s3-py/pyproject.toml has no top-level version");
    } else if (pythonProjectVersion !== goVersion) {
      problems.push(
        `version mismatch: internal/version=${goVersion}, packages/stow-s3-py=${pythonProjectVersion}`,
      );
    }
  }
  if (!pythonProjectVersion) {
    const alreadyReported = problems.some((p) => p.includes("pyproject.toml has no top-level version"));
    if (!alreadyReported) {
      problems.push("packages/stow-s3-py/pyproject.toml has no top-level version");
    }
  } else if (pythonInitVersion !== pythonProjectVersion) {
    problems.push(
      `stow_s3.__version__=${pythonInitVersion ?? "missing"} does not match pyproject version=${pythonProjectVersion}`,
    );
  }
  return problems;
}

export function registryProblems(manifest) {
 // The registry is the check; access is not. GitHub Packages reads visibility from a
  // package setting rather than from the manifest, so asserting access: public here
  // refused every manifest that was actually correct for this registry.
  return manifest.publishConfig?.registry === "https://npm.pkg.github.com"
   ? [] : [`${manifest.name} must declare the public npmjs publish destination`];
}

const root = resolve(import.meta.dirname, "..");
const goSource = readFileSync(resolve(root, "internal/version/version.go"), "utf8");
const goVersion = goSource.match(/const Version = "([^"]+)"/)?.[1];
const packageJson = JSON.parse(readFileSync(resolve(root, "packages/stow-s3/package.json"), "utf8"));
const problems = registryProblems(packageJson);

// The platform packages carry the native binary, so a version skew between them
// and the main package would publish a tarball whose binary is from a different
// release than the JavaScript that resolves it.
const PLATFORM_PACKAGES = [
  { dir: "stow-s3-linux-x64", name: "@chester-hill-solutions/stow-s3-linux-x64", os: "linux", cpu: "x64" },
  { dir: "stow-s3-linux-arm64", name: "@chester-hill-solutions/stow-s3-linux-arm64", os: "linux", cpu: "arm64" },
  { dir: "stow-s3-darwin-x64", name: "@chester-hill-solutions/stow-s3-darwin-x64", os: "darwin", cpu: "x64" },
  { dir: "stow-s3-darwin-arm64", name: "@chester-hill-solutions/stow-s3-darwin-arm64", os: "darwin", cpu: "arm64" },
];

const optional = packageJson.optionalDependencies ?? {};

// The PyPI distribution ships the same binary, so it must be in step too. A skew
// here would publish a wheel whose binary is from a different release than the
// Python code that resolves it, which is the same failure the npm platform
// packages are checked for above.
const pythonProject = readFileSync(resolve(root, "packages/stow-s3-py/pyproject.toml"), "utf8");
const pythonVersion = pythonProject.match(/^version = "([^"]+)"/m)?.[1];

// The Python package reports its own __version__, and a user comparing it with
// the binary version needs the two to be the same number.
const pythonInit = readFileSync(resolve(root, "packages/stow-s3-py/src/stow_s3/__init__.py"), "utf8");
const pythonInitVersion = pythonInit.match(/^__version__ = "([^"]+)"/m)?.[1];
problems.push(
  ...versionProblems({
    goVersion,
    packageVersion: packageJson.version,
    pythonProjectVersion: pythonVersion,
    pythonInitVersion,
  }),
);

// Outside a git work tree, such as a source export, there is no index to
// consult and the tracked-file check below is skipped.
function inGitWorkTree(cwd) {
  try {
    execFileSync("git", ["rev-parse", "--is-inside-work-tree"], { cwd, stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}
for (const platform of PLATFORM_PACKAGES) {
  const manifestPath = resolve(root, "packages", platform.dir, "package.json");
  if (!existsSync(manifestPath)) {
    problems.push(`missing platform package manifest: packages/${platform.dir}/package.json`);
    continue;
  }
  // A manifest that exists in the working tree but is untracked would pass every
  // other check here and still break a fresh clone, which is how these packages
  // were once left out of a commit while the local gate stayed green. Skipped
  // outside a git work tree, where a source export has no index to consult.
  if (inGitWorkTree(root)) {
    try {
      execFileSync("git", ["ls-files", "--error-unmatch", `packages/${platform.dir}/package.json`], {
        cwd: root,
        stdio: "ignore",
      });
    } catch {
      problems.push(`untracked platform package manifest: packages/${platform.dir}/package.json`);
      continue;
    }
  }
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  problems.push(...registryProblems(manifest));
  if (manifest.name !== platform.name) {
    problems.push(`packages/${platform.dir} is named ${manifest.name}, want ${platform.name}`);
  }
  if (manifest.version !== goVersion) {
    problems.push(`packages/${platform.dir} is version ${manifest.version}, want ${goVersion}`);
  }
  if (JSON.stringify(manifest.os) !== JSON.stringify([platform.os])) {
    problems.push(`packages/${platform.dir} declares os ${JSON.stringify(manifest.os)}, want ["${platform.os}"]`);
  }
  if (JSON.stringify(manifest.cpu) !== JSON.stringify([platform.cpu])) {
    problems.push(`packages/${platform.dir} declares cpu ${JSON.stringify(manifest.cpu)}, want ["${platform.cpu}"]`);
  }
  if (optional[platform.name] !== goVersion) {
    problems.push(`optionalDependencies.${platform.name} is ${optional[platform.name] ?? "missing"}, want ${goVersion}`);
  }
}
const declared = Object.keys(optional).filter((name) => !PLATFORM_PACKAGES.some((p) => p.name === name));
if (declared.length > 0) {
  problems.push(`unexpected optionalDependencies: ${declared.join(", ")}`);
}

// `stow doctor` reports whether a published binary exists for the running
// platform, so its list has to be the same list that actually ships. It lives in
// Go, in GOOS/GOARCH form, while the manifests use npm's cpu names, so the two
// are translated rather than compared as strings.
const NPM_CPU_TO_GOARCH = { x64: "amd64", arm64: "arm64" };
const doctorSource = readFileSync(resolve(root, "cmd/stow-s3/doctor.go"), "utf8");
const doctorList = doctorSource.match(/var supportedPlatforms = \[\]string\{([^}]*)\}/s)?.[1];
if (!doctorList) {
  problems.push("cmd/stow-s3/doctor.go no longer declares supportedPlatforms");
} else {
  const declaredPlatforms = [...doctorList.matchAll(/"([a-z0-9]+)\/([a-z0-9]+)"/g)].map((m) => `${m[1]}/${m[2]}`);
  const shippedPlatforms = PLATFORM_PACKAGES.map((p) => `${p.os}/${NPM_CPU_TO_GOARCH[p.cpu] ?? p.cpu}`);
  const missingFromDoctor = shippedPlatforms.filter((p) => !declaredPlatforms.includes(p));
  const missingFromRelease = declaredPlatforms.filter((p) => !shippedPlatforms.includes(p));
  if (missingFromDoctor.length > 0) {
    problems.push(`stow doctor omits shipped platforms: ${missingFromDoctor.join(", ")}`);
  }
  if (missingFromRelease.length > 0) {
    problems.push(`stow doctor claims platforms that are not shipped: ${missingFromRelease.join(", ")}`);
  }
}

// Both clients spawn a session's server as a child process and both decide what
// Go collector target that child runs with. A session's peak memory depends on
// it, so a silent drift between the two clients would mean the Python and
// TypeScript sessions behave differently for a reason nobody chose.
const tsSession = readFileSync(resolve(root, "packages/stow-s3/src/start.ts"), "utf8");
const tsGogc = tsSession.match(/^export const SESSION_GOGC = "(\d+)";/m)?.[1];
const pySession = readFileSync(resolve(root, "packages/stow-s3-py/src/stow_s3/session.py"), "utf8");
const pyGogc = pySession.match(/^SESSION_GOGC = "(\d+)"/m)?.[1];
if (!tsGogc) {
  problems.push("packages/stow-s3/src/start.ts no longer declares SESSION_GOGC");
} else if (!pyGogc) {
  problems.push("packages/stow-s3-py/src/stow_s3/session.py no longer declares SESSION_GOGC");
} else if (tsGogc !== pyGogc) {
  problems.push(`session GOGC differs between clients: TypeScript ${tsGogc}, Python ${pyGogc}`);
}

// Go writes the data-directory ownership marker; the TypeScript client reads it
// before deleting anything. If the names drift, every reset silently refuses
// and a developer is left with data they cannot clear through the package.
const goOwner = readFileSync(resolve(root, "internal/storage/fs/owner.go"), "utf8");
const goMarker = goOwner.match(/^const OwnerMarkerName = "([^"]+)"/m)?.[1];
const tsOwner = readFileSync(resolve(root, "packages/stow-s3/src/ownership.ts"), "utf8");
const tsMarker = tsOwner.match(/^export const STOW_OWNER_MARKER = "([^"]+)"/m)?.[1];
if (!goMarker) {
  problems.push("internal/storage/fs/owner.go no longer declares OwnerMarkerName");
} else if (!tsMarker) {
  problems.push("packages/stow-s3/src/ownership.ts no longer declares STOW_OWNER_MARKER");
} else if (goMarker !== tsMarker) {
  problems.push(`data directory ownership marker differs between writer and reader: Go ${goMarker}, TypeScript ${tsMarker}`);
}

// Go writes the versioned readiness record and both clients read it. Each side
// restates the protocol version independently and each reader rejects any other
// value, so a drift would leave one client refusing a server the other accepts -
// and the two would disagree about a record whose fields they may no longer
// understand. The build version is gated above; this is the wire version, which
// can move on its own.
const goReady = readFileSync(resolve(root, "internal/ready/ready.go"), "utf8");
const goProtocol = goReady.match(/^const ProtocolVersion = (\d+)/m)?.[1];
const tsReady = readFileSync(resolve(root, "packages/stow-s3/src/ready.ts"), "utf8");
const tsProtocol = tsReady.match(/^export const READY_PROTOCOL_VERSION = (\d+);/m)?.[1];
const pyReady = readFileSync(resolve(root, "packages/stow-s3-py/src/stow_s3/ready.py"), "utf8");
const pyProtocol = pyReady.match(/^READY_PROTOCOL_VERSION = (\d+)/m)?.[1];
if (!goProtocol) {
  problems.push("internal/ready/ready.go no longer declares ProtocolVersion");
} else if (!tsProtocol) {
  problems.push("packages/stow-s3/src/ready.ts no longer declares READY_PROTOCOL_VERSION");
} else if (!pyProtocol) {
  problems.push("packages/stow-s3-py/src/stow_s3/ready.py no longer declares READY_PROTOCOL_VERSION");
} else if (goProtocol !== tsProtocol || goProtocol !== pyProtocol) {
  problems.push(`readiness protocol version differs between writer and readers: Go ${goProtocol}, TypeScript ${tsProtocol}, Python ${pyProtocol}`);
}

// Go writes the readiness record's capability set and both clients read it. Each
// side restates the field names independently, and the record is the only place a
// caller learns what the server will enforce, so a field the writer publishes and
// a reader never declares is a capability no client can report. One had already
// gone missing this way before the gate existed: maxMultipartUploads.
//
// Names are compared case- and separator-insensitively because the wire is
// camelCase while the Python dataclass is snake_case, which is a convention
// difference rather than a disagreement.
function capabilityBlock(source, marker, terminators) {
  const start = source.indexOf(marker);
  if (start === -1) return null;
  let end = source.length;
  for (const terminator of terminators) {
    const at = source.indexOf(terminator, start + marker.length);
    if (at !== -1 && at < end) end = at;
  }
  return source.slice(start, end);
}

const capabilitySources = [
  {
    language: "Go",
    text: capabilityBlock(
      readFileSync(resolve(root, "internal/ready/ready.go"), "utf8"),
      "type Capabilities struct {",
      ["\n}"],
    ),
    names: (block) => [...block.matchAll(/json:"([A-Za-z0-9_]+)"/g)].map((m) => m[1]),
  },
  {
    language: "TypeScript",
    text: capabilityBlock(
      readFileSync(resolve(root, "packages/stow-s3/src/ready.ts"), "utf8"),
      "export interface StowReadyCapabilities {",
      ["\n}"],
    ),
    names: (block) => [...block.matchAll(/^\s{2}([A-Za-z][A-Za-z0-9_]*)\??:/gm)].map((m) => m[1]),
  },
  {
    language: "Python",
    text: capabilityBlock(
      readFileSync(resolve(root, "packages/stow-s3-py/src/stow_s3/ready.py"), "utf8"),
      "class StowCapabilities:",
      ["\n@dataclass", "\ndef ", "\nclass "],
    ),
    names: (block) => [...block.matchAll(/^\s{4}([a-z][a-z0-9_]*):/gm)].map((m) => m[1]),
  },
];

const normalize = (name) => name.toLowerCase().replace(/_/g, "");
const capabilitySets = [];
for (const source of capabilitySources) {
  if (source.text === null) {
    problems.push(`the readiness capability set is no longer declared in ${source.language}`);
    continue;
  }
  capabilitySets.push({ language: source.language, names: new Set(source.names(source.text).map(normalize)) });
}
if (capabilitySets.length === capabilitySources.length) {
  const declared = new Set(capabilitySets.flatMap((s) => [...s.names]));
  for (const name of declared) {
    const missingFrom = capabilitySets.filter((s) => !s.names.has(name)).map((s) => s.language);
    if (missingFrom.length > 0) {
      problems.push(
        `readiness capability ${name} is declared in ${capabilitySets.filter((s) => s.names.has(name)).map((s) => s.language).join(", ")} but not in ${missingFrom.join(", ")}`,
      );
    }
  }
}

if (problems.length > 0) {
  for (const problem of problems) console.error(problem);
  process.exit(1);
}
console.log(`Version source OK (${goVersion}); ${PLATFORM_PACKAGES.length} platform packages in step`);
