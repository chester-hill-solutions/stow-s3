import assert from "node:assert/strict";
import type { CorpusExpectation } from "./shared-corpus-types.js";

// Assertions shared by the corpus runners.
//
// These live in their own module because a range runner and the shared runner
// both need them, and the DRY gate is right: two copies of an error-shape reader
// is two places for the two runners to disagree about what a failure is. The
// alternative — one runner importing the other — makes the two circular, since
// the shared runner already dispatches to the range runner.

// assertMetadata checks only the keys a case names. A case that does not state
// metadata is not asserting that the response carried none.
export function assertMetadata(
  actual: Record<string, string> | undefined,
  expected: Record<string, string> | undefined,
  label: string,
): void {
  for (const [key, value] of Object.entries(expected ?? {})) {
    assert.equal(actual?.[key], value, `${label} metadata ${key}`);
  }
}

// A failure as the corpus needs to see it: the two fields a case can state, with
// everything else discarded so two runners can compare like with like.
export interface CorpusFailure {
  status: number | undefined;
  code: string | undefined;
}

export function errorStatus(error: unknown): number | undefined {
  if (!isRecord(error)) return undefined;
  const metadata = error["$metadata"];
  if (!isRecord(metadata)) return undefined;
  const status = metadata["httpStatusCode"];
  return typeof status === "number" ? status : undefined;
}

export function errorCode(error: unknown): string | undefined {
  if (!isRecord(error)) return undefined;
  for (const key of ["Code", "code", "name"]) {
    const value = error[key];
    if (typeof value === "string" && value.length > 0) return value;
  }
  const message = error["message"];
  if (typeof message !== "string") return undefined;
  return /<Code>([^<]+)<\/Code>/.exec(message)?.[1];
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

// assertFailure runs an action that is expected to fail and reduces the error to
// the fields a case states. It returns the reduced failure so a caller that needs
// to say something about it — a range refusal has to be checked against the real
// object size — can, without re-deriving the error shape.
export async function assertFailure(
  action: () => Promise<unknown>,
  expected: CorpusExpectation,
  label: string,
): Promise<CorpusFailure> {
  let failure: unknown;
  try {
    await action();
  } catch (error) {
    failure = error;
  }
  assert.ok(failure !== undefined, `expected ${label} to fail`);
  const reduced: CorpusFailure = { status: errorStatus(failure), code: errorCode(failure) };
  assert.equal(reduced.status, expected.status, `${label} status`);
  if (expected.errorCode) {
    assert.equal(reduced.code, expected.errorCode, `${label} code`);
  }
  return reduced;
}
