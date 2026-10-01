// ADR index and lifecycle gate.
//
// An ADR is a sprint artifact: it records a decision taken at a time, and decisions
// get revised. Two things then go wrong, and both have happened here.
//
// The first is that a decision is revised and the ADR that recorded it is not. ADR
// 0011 superseded ADR 0001 in full, and ADR 0001's own frontmatter still read
// `status: accepted` with no mention of it — so a reader who opened 0001 had no way
// to know it was no longer in force. The supersession was discoverable only by
// reading 0011.
//
// The second is that an ADR which has been amended is indistinguishable from one that
// has never been touched. Thirteen files all reading `status: accepted` tells a reader
// nothing about which decisions survived contact with implementation.
//
// This gate makes the index the single place a reader looks, and it makes the two
// drift cases fail. It does not prevent an ADR from being edited — that is the point
// of a sprint artifact — it requires the edit to be recorded.

import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { resolve, basename } from "node:path";
import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";

const adrDir = resolve(fileURLToPath(new URL(".", import.meta.url)), "..", "docs", "adr");
const indexPath = resolve(adrDir, "README.md");

// The vocabulary. Kept small on purpose: a status nobody can tell apart from another
// is a status that does not carry information.
const STATUSES = new Set(["accepted", "narrowed", "superseded", "proposed", "rejected"]);

// A status that is not in force still has a reason to exist on disk, and the reason is
// the reasoning. A superseded ADR is not deleted; it is filed with a pointer to what
// replaced it, so the reasoning behind a decision nobody would make today is still
// reachable.
const IN_FORCE = new Set(["accepted", "narrowed"]);

export function parseAdr(text, file) {
  const frontmatter = {};
  if (text.startsWith("---\n")) {
    const end = text.indexOf("\n---\n", 4);
    if (end === -1) {
      return { file, error: "the frontmatter block is opened with --- and never closed" };
    }
    for (const line of text.slice(4, end).split("\n")) {
      const match = /^([a-z_]+):\s*(.*)$/.exec(line);
      if (match) frontmatter[match[1]] = match[2].trim();
    }
  }
  const id = /^(\d{4})-/.exec(basename(file, ".md"))?.[1];
  const amendments = /^##\s+Amendments\s*$/m.test(text);
  return {
    file,
    id,
    status: frontmatter.status,
    supersedes: frontmatter.supersedes,
    supersededBy: frontmatter.superseded_by,
    narrowedBy: frontmatter.narrowed_by,
    amended: frontmatter.amended,
    decisionDigest: frontmatter.decision_digest,
    amendments,
    title: (/^#\s+(.+)$/m.exec(text)?.[1] ?? "").trim(),
    text,
  };
}

// adrProblems is the whole rule, as a pure function of the parsed ADRs and the index
// text, so it can be tested in every state it has rather than only by editing files.
// decisionText is the prose an amendment would change: the body, with the frontmatter
// and the Amendments section removed. Those two are where a change is *recorded*, so
// including them would make the digest change on every amendment even when the
// decision itself did not — which is the distinction the digest exists to draw.
function decisionText(text) {
  const body = text.startsWith("---\n") ? text.slice(text.indexOf("\n---\n", 4) + 5) : text;
  return body.split(/^##\s+Amendments\s*$/m)[0].trim();
}

// decisionDigest hashes the prose an amendment would change, so changing it without
// recording the change is detectable without consulting history.
//
// A digest rather than a comparison against git, because a history comparison cannot
// be validated in the commit that changes what it measures: every ADR differs from a
// parent that predates the digest, so the check would pass everything exactly once
// and start working afterwards. A digest is self-contained, survives a shallow clone,
// and is checkable the moment it is written.
export function decisionDigest(text) {
  return createHash("sha256").update(decisionText(text)).digest("hex").slice(0, 16);
}

function adrsOf() {
  return readdirSync(adrDir)
    .filter((name) => /^\d{4}-.*\.md$/.test(name))
    .map((name) => parseAdr(readFileSync(resolve(adrDir, name), "utf8"), name));
}

export function adrProblems(adrs, indexText) {
  const problems = [];
  const byId = new Map(adrs.map((adr) => [adr.id, adr]));

  for (const adr of adrs) {
    if (adr.error) {
      problems.push(`${adr.file}: ${adr.error}`);
      continue;
    }
    if (adr.id === undefined) {
      problems.push(`${adr.file}: the filename does not start with a four-digit sequence number`);
    }
    if (adr.status === undefined) {
      problems.push(`${adr.file}: no status in the frontmatter, so a reader cannot tell whether it is in force`);
      continue;
    }
    if (!STATUSES.has(adr.status)) {
      problems.push(
        `${adr.file}: status "${adr.status}" is not one of ${[...STATUSES].join(", ")}`,
      );
    }
    if (!adr.amendments) {
      problems.push(
        `${adr.file}: no "## Amendments" section, so an amended decision and an untouched one look the same`,
      );
    }
    if (adr.decisionDigest === undefined) {
      problems.push(
        `${adr.file}: no decision_digest, so a change to the decision cannot be told from no change at all`,
      );
    } else {
      const actual = decisionDigest(adr.text);
      if (adr.decisionDigest !== actual) {
        problems.push(
          `${adr.file}: the decision is ${actual} but decision_digest says ${adr.decisionDigest}, so it changed without being recorded — bump amended:, add a dated entry under Amendments, then re-run this gate with --write-digests`,
        );
      }
    }
    // A decision that is not in force has to say what replaced it, or it is a file
    // that reads as current and is not. This is the 0001 case.
    if (adr.status === "superseded" && !adr.supersededBy) {
      problems.push(
        `${adr.file}: status is superseded but nothing names what superseded it, which is the case that let ADR 0001 read as accepted after ADR 0011 replaced it`,
      );
    }
    if (IN_FORCE.has(adr.status) && adr.supersededBy) {
      problems.push(
        `${adr.file}: status is ${adr.status} but it declares superseded_by: ${adr.supersededBy}, and a decision cannot be both`,
      );
    }
    for (const field of ["supersedes", "supersededBy", "narrowedBy"]) {
      const value = adr[field];
      if (value === undefined) continue;
      const target = /^(\d{4})/.exec(value)?.[1];
      if (target === undefined) {
        problems.push(`${adr.file}: ${field} does not name an ADR by number`);
        continue;
      }
      if (!byId.has(target)) {
        problems.push(`${adr.file}: ${field} names ADR ${target}, which does not exist`);
      }
    }
    // Supersession is a claim about two files, so it has to be made in both. One side
    // is how a reader is left believing a retired decision is current.
    if (adr.supersededBy !== undefined) {
      const target = byId.get(/^(\d{4})/.exec(adr.supersededBy)?.[1] ?? "");
      const back = /^(\d{4})/.exec(target?.supersedes ?? "")?.[1];
      if (target !== undefined && back !== adr.id) {
        problems.push(
          `${adr.file} declares it was superseded by ${adr.supersededBy}, but that ADR does not declare it supersedes this one`,
        );
      }
    }
  }

  // The index is where a reader looks, so it has to be complete and it has to agree.
  if (indexText === undefined) {
    return problems;
  }
  for (const adr of adrs) {
    if (adr.id === undefined) continue;
    if (!indexText.includes(adr.file)) {
      problems.push(`${adr.file} is not listed in docs/adr/README.md, so the index is not the whole set`);
    }
    const row = indexText.split("\n").find((line) => line.includes(adr.file));
    if (row !== undefined && adr.status !== undefined) {
      const declared = /`([a-z]+)`/.exec(row)?.[1];
      if (declared !== undefined && declared !== adr.status) {
        problems.push(
          `docs/adr/README.md records ${adr.file} as ${declared} and its frontmatter says ${adr.status}`,
        );
      }
    }
  }
  return problems;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  if (process.argv.includes("--write-digests")) {
    let changed = 0;
    for (const adr of adrsOf()) {
      const path = resolve(adrDir, adr.file);
      const text = readFileSync(path, "utf8");
      const digest = decisionDigest(text);
      if (adr.decisionDigest === digest) continue;
      // Replace the digest in place, or insert it when the frontmatter has none.
      // Inserting unconditionally meant a second amendment produced a second
      // decision_digest key, and the YAML parser then read the stale one, so
      // re-digesting an ADR that had already been amended reported "updated" and
      // changed nothing that mattered. Only a first amendment could ever work.
      const next = /^decision_digest:.*$/m.test(text)
        ? text.replace(/^decision_digest:.*$/m, `decision_digest: ${digest}`)
        : text.replace(/^(status:.*)$/m, `$1\ndecision_digest: ${digest}`);
      if (next === text) continue;
      writeFileSync(path, next);
      console.log(`  ${adr.file}: ${adr.decisionDigest ?? "(none)"} -> ${digest}`);
      changed += 1;
    }
    console.log(changed === 0 ? "All decision digests already current" : `Updated ${changed}`);
    process.exit(0);
  }

  const adrs = adrsOf();

  let indexText;
  try {
    indexText = readFileSync(indexPath, "utf8");
  } catch {
    console.error("docs/adr/README.md is missing, and it is the index a reader looks in");
    process.exit(1);
  }

  const problems = adrProblems(adrs, indexText);

  // decisionText is the prose an amendment would change: the body, with the frontmatter
// and the Amendments section removed. Those two are where a change is *recorded*, so
// including them would make this check fail on the commit that does the recording —
// which is the first reconciliation's whole problem, and the reason it could not
// otherwise be landed.
function decisionText(text) {
  const body = text.startsWith("---\n") ? text.slice(text.indexOf("\n---\n", 4) + 5) : text;
  return body.split(/^##\s+Amendments\s*$/m)[0].trim();
}

  if (problems.length > 0) {
    console.error("ADR index and lifecycle do not agree:\n");
    for (const problem of problems) console.error(`  - ${problem}`);
    console.error(
      "\nAn ADR is a sprint artifact and may be amended — that is what they are for. The\n" +
        "amendment has to be recorded: bump amended:, add a dated entry under Amendments,\n" +
        "and update the row in docs/adr/README.md. A decision that changed without saying\n" +
        "so is how ADR 0001 read as accepted for a year after ADR 0011 replaced it.",
    );
    process.exit(1);
  }
  console.log(`ADR index OK (${adrs.length} decisions, ${IN_FORCE.size} statuses in force)`);
}
