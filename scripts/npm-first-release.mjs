// The one-release waiver belongs only to the separately authorized local bootstrap.
import { writeFileSync } from "node:fs";
import { basename, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { integrityOf, manifestOf, publishPlan } from "./publish-if-absent.mjs";
import { PACKAGE_NAMES, REGISTRY, REPOSITORY } from "./npm-publication.mjs";

export const FIRST_VERSION = "0.3.5";
export const ARTIFACT = "npm-first-0.3.5";
export const ACCOUNT = "wra-sol";
export const PUBLICATION_ORDER = [...PACKAGE_NAMES.slice(1), PACKAGE_NAMES[0]];

export function artifactReceipt(tarballs, source, run) {
  if (!/^[a-f0-9]{40}$/.test(source) || !/^[1-9]\d*$/.test(String(run))) throw Error("explicit source and rehearsal run required");
  const packages = tarballs.map(path => {
    const { name, version } = manifestOf(path);
    if (version !== FIRST_VERSION) throw Error("first npm exception is only for 0.3.5");
    return { name, version, file: basename(path), integrity: integrityOf(path) };
  });
  if (packages.length !== 5 || new Set(packages.map(p => p.name)).size !== 5 || PUBLICATION_ORDER.some(name => !packages.some(p => p.name === name))) {
    throw Error("first npm exception requires exactly the five Stow packages");
  }
  if (new Set(packages.map(p => p.file)).size !== 5) throw Error("artifact filenames must be unique");
  return { repository: REPOSITORY, registry: REGISTRY, version: FIRST_VERSION, source, run: String(run), liveProviderCompatibility: "NOT VERIFIED; waived only for interactive first npm 0.3.5", packages };
}

export function assertRun(run, source, workflow) {
  if (run.head_sha !== source || run.status !== "completed" || run.conclusion !== "success" ||
      run.path !== `.github/workflows/${workflow}` || run.event !== (workflow === "release.yml" ? "workflow_dispatch" : "push")) {
    throw Error(`successful exact-source ${workflow} run required`);
  }
}

export async function firstPlan(directory, receipt, source, run, lookup) {
  if (receipt.source !== source || receipt.run !== String(run) || receipt.version !== FIRST_VERSION ||
      receipt.registry !== REGISTRY || receipt.repository !== REPOSITORY || !Array.isArray(receipt.packages)) throw Error("bootstrap receipt identity mismatch");
  const tarballs = receipt.packages.map(entry => {
    if (!/^[A-Za-z0-9._-]+\.tgz$/.test(entry.file)) throw Error("unsafe artifact filename");
    const path = join(directory, entry.file);
    if (integrityOf(path) !== entry.integrity) throw Error("artifact changed since rehearsal");
    return path;
  });
  const actual = artifactReceipt(tarballs, source, run);
  if (JSON.stringify(actual) !== JSON.stringify(receipt)) throw Error("bootstrap receipt differs from exact artifacts");
  // All package identities, hashes and registry reads complete before a write.
  const plan = await publishPlan(tarballs, lookup);
  return PUBLICATION_ORDER.map(name => plan.find(step => step.name === name));
}

export async function executePlan(plan, { publish, lookup, verifyConsumers }) {
  for (const step of plan) {
    if (step.action === "publish") await publish(step);
    if ((await lookup(step.name, step.version))?.integrity !== step.integrity) throw Error(`published integrity not confirmed: ${step.name}`);
  }
  await verifyConsumers(plan);
}

export async function runFirstPlan(plan, mode, actions) {
  if (mode === "--check") return;
  if (mode !== "--publish") throw Error("explicit interactive publication mode required");
  await actions.authorize();
  await executePlan(plan, actions);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [output, ...tarballs] = process.argv.slice(2);
  if (!output || !tarballs.length || process.env.GITHUB_EVENT_NAME !== "workflow_dispatch" || process.env.GITHUB_REPOSITORY !== REPOSITORY) throw Error("receipt generation requires the repository's nonpublishing rehearsal");
  writeFileSync(output, JSON.stringify(artifactReceipt(tarballs, process.env.GITHUB_SHA, process.env.GITHUB_RUN_ID), null, 2) + "\n");
}
