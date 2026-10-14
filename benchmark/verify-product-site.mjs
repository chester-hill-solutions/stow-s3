import assert from "node:assert/strict";
import {execFileSync} from "node:child_process";
import {createHash} from "node:crypto";
import {cp,mkdir,readFile,readdir,writeFile} from "node:fs/promises";
import {join} from "node:path";

assert.equal(process.cwd(),"/src");assert.equal(process.env.BENCH_CLOUD_VERIFY,"1");
const source=JSON.parse(await readFile("benchmark/source.json","utf8"));
const hash=createHash("sha256");
async function visit(path,prefix=""){
  for(const entry of (await readdir(path,{withFileTypes:true})).sort((a,b)=>a.name.localeCompare(b.name))){
    const relative=join(prefix,entry.name),absolute=join(path,entry.name);
    if(relative==="benchmark/source.json"||relative.startsWith("packages/stow-s3/node_modules"))continue;
    if(entry.isDirectory())await visit(absolute,relative);
    else if(entry.isFile())hash.update(relative).update(await readFile(absolute));
  }
}
await visit("/src");
assert.equal(hash.digest("hex"),source.payloadSha256,"Uploaded source must match the sanitized snapshot");
const out="/out/verification";
await mkdir(out,{recursive:true});
const exec=(command,args)=>execFileSync(command,args,{stdio:"inherit"});
const evidence="docs/benchmarks/2026-10-13-competitor-performance/comparison";
exec("node",["benchmark/publish.mjs",evidence,"site/benchmark-data.json"]);
exec("npm",["ci","--ignore-scripts","--prefix","site"]);
exec("npm",["run","build","--prefix","site"]);
exec("npm",["test","--prefix","site"]);
exec("git",["init","--quiet"]);
exec("git",["add","."]);
exec("node",["scripts/check-doc-links.mjs"]);
exec("node",["benchmark/verify-site.mjs"]);
await cp("site/.build",join(out,"site-preview"),{recursive:true});
await cp("site/benchmark-data.json",join(out,"agent-storage.json"));
const data=JSON.parse(await readFile("site/benchmark-data.json","utf8"));
await writeFile(join(out,"receipt.json"),JSON.stringify({complete:true,finishedAt:new Date().toISOString(),source,
  comparisonScope:data.comparisonScope,benchmarkRun:data.runId,benchmarkSource:data.source.payloadSha256,
  checks:["S3-only dataset derived from completed Railway comparison","site build and unit tests","documentation links","Railway Chromium peer inclusion and desktop/mobile chart controls"],
  benchmarkRerun:false,published:false},null,2)+"\n");
