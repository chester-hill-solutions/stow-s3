import test from "node:test";
import assert from "node:assert/strict";
import { authorized } from "../http.mjs";
import { validateTarget } from "../implementations.mjs";
import { parseProcStat } from "../resources.mjs";
import { comparisonConfig, comparisonOrder } from "../s3.mjs";

test("private controls reject missing, wrong and differently sized tokens", () => {
  assert.equal(authorized({ headers: {} }, "token"), false);
  assert.equal(authorized({ headers: { authorization: "Bearer wrong" } }, "token"), false);
  assert.equal(authorized({ headers: { authorization: "Bearer token" } }, "token"), true);
  assert.equal(authorized({ headers: { authorization: "Bearer " } }, ""), false);
});

test("target requests restrict implementations and allocation envelope", () => {
  const valid = { target: "stow-memory", bytes: 4096, maxBytes: 268435456, maxObjects: 4096, serverConcurrency: 128 };
  assert.equal(validateTarget(valid), valid);
  assert.throws(()=>validateTarget({ ...valid, target: "../../other" }));
  assert.throws(()=>validateTarget({ ...valid, maxBytes: 2 ** 31 }));
  assert.throws(()=>validateTarget({ ...valid, serverConcurrency: 0.5 }));
});

test("CPU accounting accepts spaces and parentheses in process names", () => {
  const fields = Array(30).fill("0");
  fields[0] = "R"; fields[11] = "150"; fields[12] = "50";
  assert.equal(parseProcStat(`123 (name (with spaces)) ${fields.join(" ")}`, 100), 2);
});

test("comparison configuration rejects unknown targets and unbounded retained sets", () => {
  assert.throws(()=>comparisonConfig({ targets: ["unknown"] }));
  assert.throws(()=>comparisonConfig({ targets: ["stow-memory"], sizes: [1048576], retainedObjects: 5000 }));
  assert.throws(()=>comparisonConfig({ targets: ["stow-memory"], typo: 10 }));
  const { config } = comparisonConfig({ targets: ["stow-memory"], durationSeconds: 1, soakSeconds: 0 });
  assert.equal(config.durationSeconds, 1);
});

test("paired rounds keep matching backends adjacent and balance variant order", () => {
  const selected = ["stow-memory", "stow-memory-before", "stow-filesystem", "stow-filesystem-before"];
  const first = { memory: 0, filesystem: 0 };
  for (let repetition = 1; repetition <= 6; repetition++) {
    const order = comparisonOrder(selected, repetition, true);
    assert.equal(new Set(order).size, 4);
    for (let index = 0; index < 4; index += 2) {
      assert.equal(order[index].replace(/-before$/, ""), order[index + 1].replace(/-before$/, ""));
      if (order[index].endsWith("-before")) first[order[index].includes("-memory") ? "memory" : "filesystem"]++;
    }
  }
  assert.deepEqual(first, { memory: 3, filesystem: 3 });
  assert.throws(() => comparisonOrder(selected.slice(1), 1, true));
  assert.throws(() => comparisonConfig({ targets: selected, pairedVariants: "yes" }));
  assert.throws(() => comparisonConfig({ targets: ["stow-memory"], pairedVariants: true }));
});
