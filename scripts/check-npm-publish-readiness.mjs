#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { assertReadiness, manifestProblems, PACKAGE_NAMES, readReadiness } from "./npm-publication.mjs";

for (const name of PACKAGE_NAMES) {
  const manifest = JSON.parse(readFileSync(new URL(`../packages/${name.split("/")[1]}/package.json`, import.meta.url), "utf8"));
  const problems = manifestProblems(manifest);
  if (problems.length) throw new Error(problems.join("\n"));
}
assertReadiness(readReadiness());
console.log("All five npmjs package/trusted-publisher readiness assertions are confirmed.");
