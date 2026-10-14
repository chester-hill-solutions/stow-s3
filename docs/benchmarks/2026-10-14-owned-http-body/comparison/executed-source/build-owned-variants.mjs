import assert from "node:assert/strict";
import { mkdir,readFile,readdir,writeFile,copyFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
if(process.cwd()!=="/src"||process.env.BENCH_OWNED_BUILD!=="1")throw Error("Railway build required");
const hash=data=>createHash("sha256").update(data).digest("hex");
const archives={baseline:{file:"before-memory-staging.tar.gz",sha256:"caf321288807e6571a23186006b853d6128101e024b128d0a09f509a65120b15"},staging:{file:"before-owned-body.tar.gz",sha256:"85794ca554d1d14a61c143021797534579a62567ebbed84a5a268bfa6177f1de"}};
await mkdir("/out",{recursive:true});
async function sourceHash(root){const hash=createHash("sha256");for(const name of["go.mod","go.sum"])hash.update(name).update(await readFile(join(root,name)));async function tree(relative){for(const entry of(await readdir(join(root,relative),{withFileTypes:true})).sort((a,b)=>a.name.localeCompare(b.name))){const name=join(relative,entry.name);if(entry.isDirectory())await tree(name);else if(entry.name.endsWith(".go")&&!entry.name.endsWith("_test.go"))hash.update(name).update(await readFile(join(root,name)));}}for(const name of["cmd","internal","pkg"])await tree(name);return hash.digest("hex");}
const variants={};
for(const variant of["baseline","staging","after"]){
  const root=variant==="after"?"/src":`/tmp/stow-${variant}`;
  if(variant!=="after"){
    const archive=archives[variant],path=join("/src/benchmark/variants",archive.file);assert.equal(hash(await readFile(path)),archive.sha256);
    await mkdir(root);execFileSync("tar",["-xzf",path,"-C",root]);
    await copyFile("/src/internal/s3api/owned_body_allocation_test.go",join(root,"internal/s3api/owned_body_allocation_test.go"));
  }
  const env={...process.env,CGO_ENABLED:"1"};
  const tests=["test","-race","./internal/storage/...","./internal/runtime/...","./internal/s3api/...","./pkg/stow/..."];
  if(variant!=="after")tests.push("-skip","^TestAuthenticatedHTTPPutAllocationBounded$");
  execFileSync("go",tests,{cwd:root,env,stdio:"inherit"});
  const server=`/out/stow-s3-${variant}`,bench=`/out/http-${variant}.test`;
  execFileSync("go",["build","-trimpath","-ldflags","-s -w","-o",server,"./cmd/stow-s3"],{cwd:root,env:{...env,CGO_ENABLED:"0"},stdio:"inherit"});
  execFileSync("go",["test","-c","-trimpath","-o",bench,"./internal/s3api"],{cwd:root,env,stdio:"inherit"});
  variants[variant]={productionSourceSha256:await sourceHash(root),serverSha256:hash(await readFile(server)),benchmarkSha256:hash(await readFile(bench)),racePassed:true,...(variant!=="after"?{sourceArchiveSha256:archives[variant].sha256,excludedTests:["TestAuthenticatedHTTPPutAllocationBounded"]}:{})};
}
assert.equal(variants.baseline.serverSha256,"7c80ec1b79aa0d457e2c4f7e7eb680ae2a8f13bf060fef2961a86d170dca70a6","pre-staging binary drift");
await writeFile("/out/variants.json",JSON.stringify({variants,source:JSON.parse(await readFile("benchmark/source.json","utf8")),go:execFileSync("go",["version"],{encoding:"utf8"}).trim()},null,2)+"\n");
