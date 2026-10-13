import assert from "node:assert/strict";
import { test } from "node:test";
import { adrProblems, decisionDigest, parseAdr } from "./check-adr-index.mjs";

function decision(id, extra = "", body = "A current decision.") {
  const text = `---\nstatus: accepted\n${extra}---\n\n# Decision ${id}\n\n${body}\n\n## Amendments\n\nNone.\n`;
  return parseAdr(text.replace("status: accepted\n", `status: accepted\ndecision_digest: ${decisionDigest(text)}\n`), `${id}-decision.md`);
}

const retired = [{ id: "0001", supersededBy: "0011", reason: "Explicit mode replaced detection", source: "https://github.com/chester-hill-solutions/stow-s3/blob/302900cf422439f4d4082d633dce2a22e79837c4/docs/adr/0001-auto-detect-run-through.md" }];
const index = "| [0011](0011-decision.md) | `accepted` |\n| 0001 | Removed | 0011 |";

test("a removed decision remains auditable through its in-force replacement", () => {
  assert.deepEqual(adrProblems([decision("0011", "supersedes: 0001\n")], index, retired), []);
});

test("deleting a referenced ADR without a retirement record fails", () => {
  assert.match(adrProblems([decision("0011", "supersedes: 0001\n")], index).join("\n"), /does not exist/);
});

test("a retirement cannot mask a live ID or omit its reciprocal replacement", () => {
  assert.match(adrProblems([decision("0001"), decision("0011")], undefined, retired).join("\n"), /removed decision/);
  assert.match(adrProblems([decision("0011")], index, retired).join("\n"), /must declare supersedes/);
});

test("changed decision prose and stale index status are rejected", () => {
  const adr = decision("0011");
  const edited = parseAdr(adr.text.replace("A current decision.", "A different decision."), adr.file);
  assert.match(adrProblems([edited], index).join("\n"), /changed without being recorded/);
  assert.match(adrProblems([adr], index.replace("`accepted`", "`proposed`")).join("\n"), /frontmatter says accepted/);
});

test("an extra frontmatter delimiter cannot hide metadata outside the YAML block", () => {
  const adr = decision("0011");
  const malformed = parseAdr(adr.text.replace("---\n", "---\n---\n"), adr.file);
  assert.match(adrProblems([malformed]).join("\n"), /no status/);
});

test("retirement history must point at an immutable revision", () => {
  const mutable = [{ ...retired[0], source: retired[0].source.replace("302900cf422439f4d4082d633dce2a22e79837c4", "main") }];
  assert.match(adrProblems([decision("0011", "supersedes: 0001\n")], index, mutable).join("\n"), /Git source link/);
});
