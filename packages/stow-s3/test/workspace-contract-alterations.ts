// The three ways a step's apply can be handed something other than the document
// the scenario produced.
//
// Split out of workspace-contract-runner.ts because that file had grown past the
// size gate, and because these share one property the rest of the driver does not:
// each builds a document that is still *valid* JSON of the right shape. A corrupted
// byte would be refused by the decoder, which is a different refusal and would make
// the digest look like it was working when it was not the thing under test.

import { readFile, writeFile } from "node:fs/promises";

import { substitute, workPath } from "./workspace-contract-runner.js";
import type { ContractRename, ContractStep, Run } from "./workspace-contract-runner.js";

/**
 * The document a step's apply reads: the real one, a corrupted copy, or a
 * substituted one. A step says which by naming the alteration, and the default is
 * the document the scenario produced.
 */
export async function documentFor(
  run: Run,
  step: ContractStep,
  declared: string,
): Promise<string> {
  if (step.tamper !== undefined) {
    return tamper(run, step);
  }
  if (step.renameInTransit !== undefined) {
    return renameInTransit(run, step.renameInTransit);
  }
  return declared;
}

/**
 * Substitutes one byte of one file's payload and re-encodes, so the document stays
 * well formed and only its content stops matching the digest it carries. A raw byte
 * flip usually lands in the JSON and is caught by the parser, which would make the
 * test pass for a reason that has nothing to do with integrity.
 */
export async function tamper(run: Run, step: ContractStep): Promise<string> {
  const declared = step.tamper;
  if (declared === undefined) {
    throw new Error(`step ${JSON.stringify(step.id)}: nothing to tamper with`);
  }
  const source = workPath(run, declared);
  const document = JSON.parse(await readFile(source, "utf8")) as Record<string, unknown>;
  const content = document["content"] as Record<string, string> | undefined;
  if (content === undefined || Object.keys(content).length === 0) {
    throw new Error(`${source} carries no content, so there is nothing to substitute`);
  }
  const [name] = Object.keys(content).sort();
  if (name === undefined) {
    throw new Error(`${source} carries no content, so there is nothing to substitute`);
  }
  const encoded = content[name];
  if (encoded === undefined) {
    throw new Error(`${source}: ${name} has no content`);
  }
  const payload = Buffer.from(encoded, "base64");
  const first = payload.at(0);
  if (first === undefined) {
    throw new Error(`${source}: the content of ${name} is empty, so there is nothing to substitute`);
  }
  payload[0] = first ^ 0x01;
  content[name] = payload.toString("base64");
  const target = workPath(run, `tampered-${declared}`);
  await writeFile(target, JSON.stringify(document));
  return target;
}

/**
 * Builds the substitution the document digest exists to catch: a change's declared
 * path moves, in the change list and in the change's own to-metadata, and the
 * payload moves with it in the content map.
 *
 * All three have to move together or the document stops being self-consistent and
 * the decoder refuses it, which is a different refusal and would make the digest
 * look like it was working when it was not the thing under test. What is left
 * passes every check the document makes about itself — the per-file content digest
 * still matches the bytes it carries, and an addition's precondition passes because
 * the new name is absent from the base — so a digest over the document as a whole
 * is the only thing that can catch it.
 */
export async function renameInTransit(run: Run, rename: ContractRename): Promise<string> {
  const source = workPath(run, substitute(run, rename.from));
  const document = JSON.parse(await readFile(source, "utf8")) as Record<string, unknown>;

  const changes = document["changes"];
  if (!Array.isArray(changes)) {
    throw new Error(`${source} carries no change list, so there is nothing to substitute`);
  }
  let moved = false;
  for (const entry of changes) {
    const change = entry as Record<string, unknown>;
    if (change["path"] !== rename.path) {
      continue;
    }
    change["path"] = rename.to;
    const target = change["to"];
    if (typeof target === "object" && target !== null && !Array.isArray(target)) {
      (target as Record<string, unknown>)["path"] = rename.to;
    }
    moved = true;
  }
  if (!moved) {
    throw new Error(`${source} has no change naming ${JSON.stringify(rename.path)}`);
  }

  const content = document["content"];
  if (typeof content !== "object" || content === null || Array.isArray(content)) {
    throw new Error(`${source} carries no content map, so a rename cannot stay self-consistent`);
  }
  const map = content as Record<string, unknown>;
  if (map[rename.path] === undefined) {
    throw new Error(`${source} carries no content for ${JSON.stringify(rename.path)}`);
  }
  map[rename.to] = map[rename.path];
  delete map[rename.path];

  const target = workPath(run, "renamed.stowdelta");
  await writeFile(target, JSON.stringify(document));
  return target;
}
