import assert from "node:assert/strict";
import { test } from "node:test";
import { runReceiverContinuation } from "../../../examples/workspace-resources/receiver-continuation.mjs";

test("portable checkpoint adoption exposes exact bytes under the receiver identity", async () => {
  const result = await runReceiverContinuation();
  assert.equal(result.status, "passed");
  assert.equal(result.source_paths_removed, true);
  assert.notEqual(result.source_workspace_id, result.receiver_workspace_id);
  assert.equal(result.imported_source_checkpoint_id, result.source_checkpoint_id);
  assert.notEqual(result.receiver_checkpoint_id, result.imported_source_checkpoint_id);
  assert.ok(result.resource_root_uri.includes(`/workspaces/${result.receiver_workspace_id}/checkpoints/${result.receiver_checkpoint_id}/`));
  assert.deepEqual(Object.keys(result.receiver_saved_files).sort(), ["inputs/evidence.csv", "inputs/progress.txt"]);
  for (const file of Object.values(result.receiver_saved_files)) {
    assert.equal(file.exact_content_verified, true);
    assert.ok(file.byte_length > 0);
    assert.match(file.sha256, /^[a-f0-9]{64}$/);
  }
});
