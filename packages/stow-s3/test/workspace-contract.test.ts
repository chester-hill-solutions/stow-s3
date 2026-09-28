// The workspace contract, asserted through the TypeScript wrapper.
//
// conformance/workspace/cases.json is the contract. This asserts the wrapper
// passes each document through unchanged, and the Go driver asserts the same
// declarations against the binary directly. The wrappers were never run against
// the binary they wrap, and both shipped the same bug: `handoff --output` prints
// nothing, and both decoded stdout anyway, so a caller asking for a path got a
// JSON parse error instead of the document it had just asked for.

import assert from "node:assert/strict";
import { after, before, describe, it } from "node:test";

import { loadContract, runContract } from "./workspace-contract-runner.js";
import type { Contract, ContractProblem } from "./workspace-contract-runner.js";
import { checkField, lookupField, sameJSON } from "./workspace-contract-matchers.js";

describe("workspace contract", () => {
  let contract: Contract;

  before(async () => {
    contract = await loadContract();
  });

  it("reads a case file the other two drivers also read", () => {
    assert.equal(contract.version, 1);
    assert.ok(contract.steps.length > 0, "the contract declares no steps, so it asserts nothing");
  });

  it("declares an expectation or a refusal for every step that can assert one", () => {
    for (const step of contract.steps) {
      if (step.verb === "noop") {
        continue;
      }
      const declared = Object.keys(step.expect ?? {}).length > 0 || step.fail !== undefined;
      assert.ok(declared, `step ${JSON.stringify(step.id)} asserts nothing`);
    }
  });

  it("the wrapper satisfies every step", async () => {
    const problems = await runContract(contract);
    assert.deepEqual(
      problems,
      [],
      problems.map((problem) => describeProblem(problem)).join("\n\n"),
    );
  });
});

/**
 * A test that stops at the first failing step is a test that hides the rest, and a
 * wrapper disagreeing with the engine in three places should read as three
 * disagreements. The runner already stops the scenario — the steps share a registry,
 * so continuing past a failure only reports its consequences — so everything the
 * run found is reported here together.
 */
function describeProblem(problem: ContractProblem): string {
  return `  ${problem.step}\n    ${problem.field} ${problem.message}`;
}

// The matchers are exercised on their own as well, because a matcher with a bug
// reports a wrapper disagreement that is not one. These are the properties the
// contract depends on and nothing else.
describe("workspace contract matchers", () => {
  it("resolves a field path with indexes", () => {
    const document = { changes: [{ path: "notes.txt", kind: "added" }] };
    assert.equal(lookupField(document, "changes[0].path"), "notes.txt");
    assert.equal(lookupField(document, "changes[0].kind"), "added");
    assert.equal(lookupField(document, "changes[1].path"), undefined);
    assert.equal(lookupField(document, "changes[0].missing"), undefined);
    assert.equal(lookupField(document, "changes.path"), undefined);
  });

  it("treats a malformed index as a missing field rather than throwing", () => {
    // Both of these name a field that is not there. The point is that neither
    // throws: a typo in the case file has to be reported as a missing field, which
    // names the contract, rather than as a parse error that names neither.
    assert.equal(lookupField({ changes: [] }, "changes[x].path"), undefined);
    assert.equal(lookupField({ changes: [{ path: "a" }] }, "changes[0"), undefined);
  });

  it("compares documents structurally, not by serialised bytes", () => {
    assert.ok(sameJSON({ a: 1, b: 2 }, { b: 2, a: 1 }));
    assert.ok(sameJSON({ n: 2 }, { n: 2.0 }));
    assert.ok(sameJSON([1, 2], [1, 2]));
    assert.ok(!sameJSON([1, 2], [2, 1]));
    assert.ok(!sameJSON({ a: 1 }, { a: 1, b: 2 }));
    assert.ok(!sameJSON({ a: 1 }, { a: "1" }));
  });

  it("reports every problem with one field, not just the first", () => {
    const problems = checkField("files", 3, { equals: 4, minLength: 5 }, (text) => text);
    assert.equal(problems.length, 2);
  });

  it("reports a field that is absent when the contract says it should be", () => {
    assert.deepEqual(checkField("checkpoint_id", "cp_1", { absent: true }, (t) => t), [
      { field: "checkpoint_id", message: 'should be absent, but it is "cp_1"' },
    ]);
    assert.deepEqual(checkField("checkpoint_id", undefined, { absent: true }, (t) => t), []);
  });

  it("distinguishes a missing field from one holding a falsy value", () => {
    assert.equal(checkField("bytes", 0, { equals: 0 }, (t) => t).length, 0);
    assert.equal(checkField("bytes", undefined, { equals: 0 }, (t) => t).length, 1);
    assert.equal(checkField("destroyed", false, { equals: false }, (t) => t).length, 0);
  });

  it("resolves captures inside an expected value", () => {
    const problems = checkField("root", "/tmp/a", { equals: "{{work}}/a" }, (text) =>
      text.replaceAll("{{work}}", "/tmp"),
    );
    assert.deepEqual(problems, []);
  });
});

after(() => {
  // Nothing to clean: the runner's work directory is a mkdtemp under the OS temp
  // dir, which is where the Go driver's t.TempDir() lands too.
});
