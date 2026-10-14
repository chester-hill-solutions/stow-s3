import assert from "node:assert/strict";
import {execFileSync,spawnSync} from "node:child_process";
import {readFile,writeFile,mkdir,rm} from "node:fs/promises";
import {createHash} from "node:crypto";
import {gzipSync,brotliCompressSync,constants} from "node:zlib";

assert.equal(process.cwd(),"/src");assert.equal(process.env.BENCH_CLOUD_VERIFY,"1");
const out="/out/verification/size";
await mkdir(out,{recursive:true});
const original=await readFile("benchmark/reference-filesystem.go.txt");
const paths=["filesystem.go","filesystem_native.go","filesystem_unsupported.go"].map(name=>`pkg/stow/${name}`);
const current=await Promise.all(paths.map(path=>readFile(path)));
const env={...process.env,GOOS:"js",GOARCH:"wasm"};
const exec=(args,options={})=>execFileSync("go",args,{stdio:"inherit",...options});
const deps=()=>execFileSync("go",["list","-deps","./cmd/stow-wasm"],{env,encoding:"utf8"});
const build=name=>exec(["build","-buildvcs=false","-trimpath","-ldflags","-s -w","-o",`${out}/${name}.wasm`,"./cmd/stow-wasm"],{env});
const info=async name=>{
  const bytes=await readFile(`${out}/${name}.wasm`);
  return {bytes:bytes.length,sha256:createHash("sha256").update(bytes).digest("hex"),
    gzipBytes:gzipSync(bytes,{level:9}).length,
    brotliBytes:brotliCompressSync(bytes,{params:{[constants.BROTLI_PARAM_QUALITY]:11}}).length};
};
let before,beforeDependencies;
try{
  await writeFile(paths[0],original);
  await Promise.all(paths.slice(1).map(path=>rm(path)));
  beforeDependencies=deps();
  build("before");before=await info("before");
  const baseline=JSON.parse(await readFile("docs/benchmarks/2026-10-13-competitor-performance/comparison/run.json","utf8"));
  const expected=baseline.metadata.build.wasmSha256;
  assert.equal(before.sha256,expected,"A/B baseline must reproduce the previously tested SDK artifact");
  const red=spawnSync("go",["test","./pkg/stow","-run","^TestWasmExcludesNativeFilesystemDependencies$","-count=1"],{encoding:"utf8"});
  assert.equal(red.status,1,"Regression test must fail on the original import boundary");
  await writeFile(`${out}/regression-red.txt`,red.stdout+red.stderr);
}finally{await Promise.all(paths.map((path,index)=>writeFile(path,current[index])));}
const afterDependencies=deps();
assert.ok(!afterDependencies.includes("/internal/storage/fs\n"));
assert.ok(!afterDependencies.includes("google.golang.org/protobuf/"));
build("after");const after=await info("after");
assert.ok(after.bytes<before.bytes,"Removing the native import must reduce WASM size");
exec(["build","./pkg/stow"],{env:{...process.env,GOOS:"windows",GOARCH:"amd64"}});
await writeFile(`${out}/before-dependencies.txt`,beforeDependencies);
await writeFile(`${out}/after-dependencies.txt`,afterDependencies);
await writeFile(`${out}/result.json`,JSON.stringify({complete:true,toolchain:execFileSync("go",["version"],{encoding:"utf8"}).trim(),
  before,after,savedBytes:before.bytes-after.bytes,removedDependencies:beforeDependencies.trim().split("\n").filter(name=>!afterDependencies.trim().split("\n").includes(name)),
  method:"Same source and release flags, changing only the native filesystem import boundary; baseline SHA matches the tested SDK. Build and compression run on Railway."},null,2)+"\n");
await rm(`${out}/before.wasm`);await rm(`${out}/after.wasm`);
console.log(JSON.stringify({wasmSizeBefore:before.bytes,wasmSizeAfter:after.bytes,savedBytes:before.bytes-after.bytes}));
