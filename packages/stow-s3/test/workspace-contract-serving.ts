import { serveWorkspace } from "../src/workspace-serve.js";
import type { WorkspaceJSON } from "../src/workspace.js";

export async function serveForContract(id: string, registryDir?: string, team?: string): Promise<WorkspaceJSON> {
  const serving = await serveWorkspace({ id, registryDir, team });
  try { return serving.ready; }
  finally { await serving.close(); }
}
