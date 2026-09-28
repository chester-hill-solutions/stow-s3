// The matchers the workspace contract uses.
//
// This is a translation of conformance/workspace_contract_judge_test.go's matcher
// set, not a second contract. The case file is the contract; these are the three
// things that can read it. Keeping them in their own module is what lets the
// scenario runner read as the scenario it is.

/** One declared property of one field, as the case file states it. */
export interface ContractExpectation {
  equals?: unknown;
  notEquals?: unknown;
  matches?: string;
  length?: number;
  minLength?: number;
  min?: number;
  absent?: boolean;
}

/** One failure of one assertion, so a step reports everything wrong with it. */
export interface ContractProblem {
  readonly field: string;
  readonly message: string;
}

/** A JSON value, kept in the shape the engine printed. */
export type JsonValue = unknown;

/** Resolves the {{...}} references in a declared string. */
export type Substitute = (text: string) => string;

function isPlainObject(value: JsonValue): value is Record<string, JsonValue> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * Resolves a dotted field path with optional indexes, so a case can say
 * "changes[0].path" instead of this file knowing the shape of a diff.
 *
 * Returns undefined for a path that does not resolve, which is also what a field
 * holding a literal undefined would look like — a distinction the case file cannot
 * express and does not need to.
 */
export function lookupField(document: JsonValue, path: string): JsonValue | undefined {
  let current: JsonValue = document;
  for (const segment of path.split(".")) {
    current = resolveSegment(current, segment);
    if (current === undefined) {
      return undefined;
    }
  }
  return current;
}

/** Resolves one "name" or "name[0][1]" segment, or undefined if it does not resolve. */
function resolveSegment(container: JsonValue, segment: string): JsonValue | undefined {
  const cut = segment.indexOf("[");
  const name = cut < 0 ? segment : segment.slice(0, cut);
  if (!isPlainObject(container)) {
    return undefined;
  }
  const value = container[name];
  if (value === undefined || cut < 0) {
    return value;
  }
  return applyIndexes(value, parseIndexes(segment.slice(cut)));
}

function applyIndexes(value: JsonValue, indexes: number[] | undefined): JsonValue | undefined {
  if (indexes === undefined) {
    return undefined;
  }
  let current = value;
  for (const index of indexes) {
    if (!Array.isArray(current) || index >= current.length) {
      return undefined;
    }
    current = current[index];
  }
  return current;
}

/**
 * Reads the "[0][1]" tail of a field path, or undefined if the tail is malformed.
 *
 * Undefined rather than an empty list is the point: an unparseable tail must leave
 * the field unresolved, because the alternative is that a typo silently resolves to
 * the container instead and the assertion quietly stops testing the field it names.
 * The Go driver does the same, and the case file is shared, so the two have to agree
 * about what a typo means.
 */
function parseIndexes(tail: string): number[] | undefined {
  const indexes: number[] = [];
  let rest = tail;
  while (rest.startsWith("[")) {
    const closing = rest.indexOf("]");
    if (closing < 0) {
      return undefined;
    }
    const parsed = Number.parseInt(rest.slice(1, closing), 10);
    if (Number.isNaN(parsed)) {
      return undefined;
    }
    indexes.push(parsed);
    rest = rest.slice(closing + 1);
  }
  return rest === "" ? indexes : undefined;
}

/**
 * Compares two JSON values structurally, so two documents that differ only in key
 * order are equal. A contract that compared serialised bytes would fail on a
 * reformat, which is a difference in encoding and not in meaning.
 */
export function sameJSON(want: JsonValue, got: JsonValue): boolean {
  if (typeof want === "number" && typeof got === "number") {
    return want === got;
  }
  if (Array.isArray(want) || Array.isArray(got)) {
    if (!Array.isArray(want) || !Array.isArray(got) || want.length !== got.length) {
      return false;
    }
    return want.every((child, index) => sameJSON(child, got[index]));
  }
  if (isPlainObject(want) && isPlainObject(got)) {
    const wantKeys = Object.keys(want).sort();
    const gotKeys = Object.keys(got).sort();
    if (wantKeys.length !== gotKeys.length) {
      return false;
    }
    return wantKeys.every(
      (key, index) => key === gotKeys[index] && sameJSON(want[key], got[key]),
    );
  }
  return want === got;
}

function lengthOf(value: JsonValue): number {
  if (Array.isArray(value) || typeof value === "string") {
    return value.length;
  }
  if (isPlainObject(value)) {
    return Object.keys(value).length;
  }
  return -1;
}

function show(value: JsonValue): string {
  return typeof value === "string" ? JSON.stringify(value) : JSON.stringify(value) ?? "undefined";
}

/**
 * Checks one declared field against one expectation and returns every problem with
 * it, rather than stopping at the first. A step that reports one failure at a time
 * makes a driver disagreeing with the engine look like several defects.
 */
export function checkField(
  field: string,
  value: JsonValue | undefined,
  want: ContractExpectation,
  substitute: Substitute,
): ContractProblem[] {
  if (want.absent) {
    return value === undefined ? [] : [{ field, message: `should be absent, but it is ${show(value)}` }];
  }
  if (value === undefined) {
    return [{ field, message: "is missing from the result" }];
  }
  return [
    ...checkEquals(field, value, want, substitute),
    ...checkNotEquals(field, value, want, substitute),
    ...checkPattern(field, value, want),
    ...checkLength(field, value, want),
    ...checkMin(field, value, want),
  ];
}

function checkEquals(
  field: string,
  value: JsonValue,
  want: ContractExpectation,
  substitute: Substitute,
): ContractProblem[] {
  if (want.equals === undefined) {
    return [];
  }
  const expected = substituteExpectation(want.equals, substitute);
  if (sameJSON(expected, value)) {
    return [];
  }
  return [{ field, message: `is ${show(value)}, and the contract says ${show(expected)}` }];
}

function checkNotEquals(
  field: string,
  value: JsonValue,
  want: ContractExpectation,
  substitute: Substitute,
): ContractProblem[] {
  if (want.notEquals === undefined) {
    return [];
  }
  const expected = substituteExpectation(want.notEquals, substitute);
  if (!sameJSON(expected, value)) {
    return [];
  }
  return [{ field, message: `is ${show(value)}, and the contract says it must differ from that` }];
}

function checkPattern(
  field: string,
  value: JsonValue,
  want: ContractExpectation,
): ContractProblem[] {
  if (want.matches === undefined) {
    return [];
  }
  if (typeof value !== "string") {
    return [{ field, message: `holds ${typeof value}, and a pattern needs a string` }];
  }
  if (new RegExp(want.matches).test(value)) {
    return [];
  }
  return [
    { field, message: `is ${show(value)}, which does not match ${JSON.stringify(want.matches)}` },
  ];
}

function checkLength(
  field: string,
  value: JsonValue,
  want: ContractExpectation,
): ContractProblem[] {
  const atLeast = want.minLength;
  const exactly = want.length;
  if (atLeast === undefined && exactly === undefined) {
    return [];
  }
  const length = lengthOf(value);
  if (exactly !== undefined && length !== exactly) {
    return [{ field, message: `holds ${length} entries, and the contract says ${exactly}` }];
  }
  if (atLeast !== undefined && length < atLeast) {
    return [{ field, message: `holds ${length} entries, and the contract says at least ${atLeast}` }];
  }
  return [];
}

function checkMin(field: string, value: JsonValue, want: ContractExpectation): ContractProblem[] {
  if (want.min === undefined) {
    return [];
  }
  if (typeof value !== "number") {
    return [{ field, message: `holds ${typeof value}, and a bound needs a number` }];
  }
  if (value >= want.min) {
    return [];
  }
  return [{ field, message: `is ${value}, and the contract says at least ${want.min}` }];
}

/**
 * Resolves the {{...}} references inside an expected value, so a step can state
 * "the root the manifest named" rather than repeating a path it already declared.
 * A non-string expected value has nothing to substitute.
 */
function substituteExpectation(value: JsonValue, substitute: Substitute): JsonValue {
  return typeof value === "string" ? substitute(value) : value;
}
