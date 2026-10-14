import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {pathToFileURL} from "node:url";

export function releasePlan({version,packageVersion,publishPackages,notes}) {
  const normalized=version?.replace(/^v/,"");
  if(!/^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(normalized??"")) throw new Error("Release version must be explicit semver");
  if(normalized!==packageVersion) throw new Error("Release tag and package version differ");
  if(typeof publishPackages!=="boolean") throw new Error("Release policy must explicitly choose package publication");
  if(!notes?.trim()) throw new Error("Release notes must exist before publication");
  return {version:normalized,publishPackages,notesFile:`docs/releases/v${normalized}.md`};
}

if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href) {
  const root=resolve(import.meta.dirname,"..");
  const policy=JSON.parse(readFileSync(resolve(root,"scripts/release-policy.json"),"utf8"));
  const manifest=JSON.parse(readFileSync(resolve(root,"packages/stow-s3/package.json"),"utf8"));
  const version=process.env.RELEASE_VERSION;
  const normalized=version?.replace(/^v/,"");
  if(!/^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(normalized??"")) throw new Error("Release version must be explicit semver");
  const plan=releasePlan({version,packageVersion:manifest.version,publishPackages:policy.publishPackages,
    notes:readFileSync(resolve(root,`docs/releases/v${normalized}.md`),"utf8")});
  console.log(`version=${plan.version}\npublish_packages=${plan.publishPackages}\nnotes_file=${plan.notesFile}`);
}
