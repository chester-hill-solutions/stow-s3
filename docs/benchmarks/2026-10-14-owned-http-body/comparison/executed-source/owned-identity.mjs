import {readFile,writeFile}from"node:fs/promises";
import{createHash}from"node:crypto";
const hash=async path=>createHash("sha256").update(await readFile(path)).digest("hex");
const specification=JSON.parse(await readFile("bin/variants.json","utf8"));
const binaries={};for(const variant of["baseline","staging","after"]){binaries[`stow-s3-${variant}`]=await hash(`bin/stow-s3-${variant}`);binaries[`http-${variant}.test`]=await hash(`bin/http-${variant}.test`);if(binaries[`stow-s3-${variant}`]!==specification.variants[variant].serverSha256)throw Error("variant identity mismatch");}
await writeFile("benchmark/build-identity.json",JSON.stringify({binaries,variants:specification.variants,source:specification.source,
  wasmSha256:await hash("packages/stow-s3/dist/stow-runtime.wasm"),sdkLockfileSha256:await hash("packages/stow-s3/package-lock.json"),wasmIncludedButNotExecuted:true},null,2)+"\n");
