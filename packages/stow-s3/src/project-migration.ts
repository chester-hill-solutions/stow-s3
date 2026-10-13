import type { BrowserFileHistory } from "./file-history.js";
import type { BrowserProject } from "./project.js";
import { contentHash } from "./content-store.js";
import { ProjectError, type ProjectStatus } from "./project-types.js";

/** Copies a legacy file's retained stages without deleting or advancing its source. */
export async function migrateFileHistory(source: Pick<BrowserFileHistory, "exportRetained">, key: string, target: BrowserProject): Promise<ProjectStatus> {
  let selected: string | undefined;
  let migrationPrefix: string | undefined;
  const targetFiles = await target.listFiles();
  if (targetFiles.some((file) => file.key !== key)) throw new ProjectError("invalid_options", "legacy history migration requires a dedicated single-file project");
  await source.exportRetained(key, async (version, bytes, history) => {
    const fingerprint = await contentHash(new TextEncoder().encode(JSON.stringify(history)));
    migrationPrefix = `legacy/${history.remoteBucket}/${encodeURIComponent(key)}/${fingerprint}`;
    const status = await target.status();
    const token = `${migrationPrefix}/${version.id}`;
    const previous = await target.migrationProgress(token);
    if (target.retentionLimit < history.versions.length) throw new ProjectError("invalid_options", "target retention must accommodate the complete source history");
    if (version.id === history.versions[0]?.id && status.versions.length > 0 && !previous) throw new ProjectError("invalid_options", "migration source changed or target already contains unrelated history");
    if (previous && !status.versions.some((item) => item.id === previous)) throw new ProjectError("invalid_options", "migration target pruned an imported revision; increase retention before retrying");
    const result = await target.commit([{ type: "put", key, data: bytes, contentType: version.contentType, metadata: version.metadata }], {
      label: version.label, migrationToken: token,
    });
    const importedId = await target.migrationProgress(token);
    if (!importedId || !result.versions.some((item) => item.id === importedId)) throw new ProjectError("invalid_options", "migration retention is insufficient for the source history");
    if (version.id === history.currentVersionId) selected = importedId;
  });
  if (!selected || !migrationPrefix) throw new ProjectError("not_found", "source history is empty");
  return target.checkout(selected);
}
