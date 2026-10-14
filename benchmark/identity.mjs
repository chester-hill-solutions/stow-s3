import { readFile, writeFile, readdir } from "node:fs/promises";
import { createHash } from "node:crypto";
import { join } from "node:path";

const binaries = {};
const variants = await readFile("generated/build-variants.json", "utf8").then(JSON.parse).catch(error => { if (error.code === "ENOENT") return null; throw error; });
for (const name of variants ? ["stow-s3", "stow-s3-before", "embedded.test", "embedded-before.test"] : ["stow-s3", "minio", "rustfs", "s3proxy.jar", "embedded.test"]) binaries[name] = createHash("sha256").update(await readFile(`bin/${name}`)).digest("hex");
const sources = createHash("sha256");
for (const root of ["benchmark", "packages/stow-s3/scripts/performance"]) {
  for (const name of (await readdir(root)).filter(n=>n.endsWith(".mjs")).sort()) sources.update(await readFile(join(root, name)));
}
const source = await readFile("benchmark/source.json", "utf8").then(JSON.parse).catch(() => null);
await writeFile("benchmark/build-identity.json", JSON.stringify({ binaries, ...(variants ? {variants} : {}), harnessSha256: sources.digest("hex"),
  versions: JSON.parse(await readFile("benchmark/versions.json", "utf8")), source,
  wasmSha256: createHash("sha256").update(await readFile("packages/stow-s3/dist/stow-runtime.wasm")).digest("hex"),
  benchmarkModuleSumSha256: createHash("sha256").update(await readFile("generated/benchmark-go.sum")).digest("hex"),
  sdkLockfileSha256: createHash("sha256").update(await readFile("packages/stow-s3/package-lock.json")).digest("hex") }, null, 2) + "\n");
