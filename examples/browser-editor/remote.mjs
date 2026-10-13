import { FileHistoryError } from "/runtime/browser.js";

/** A same-origin host proxy holds the S3 credentials in this demo. */
export function editorRemote(isOffline) {
  function requireOnline() {
    if (isOffline()) throw new Error("Working offline. Your local stages are saved; reconnect to sync.");
  }
  return {
    async getObject(_bucket, key) {
      requireOnline();
      const response = await fetch(`/api/object?key=${encodeURIComponent(key)}`);
      if (response.status === 404) return null;
      if (!response.ok) throw new Error(`Remote read failed (${response.status})`);
      return {
        data: new Uint8Array(await response.arrayBuffer()), etag: response.headers.get("etag"),
        saveId: response.headers.get("x-stow-save-id"), contentType: response.headers.get("content-type"),
        metadata: JSON.parse(response.headers.get("x-stow-metadata") ?? "{}"),
      };
    },
    async putObject(_bucket, key, data, options) {
      requireOnline();
      const response = await fetch(`/api/object?key=${encodeURIComponent(key)}`, {
        method: "PUT", body: data,
        headers: {
          ...("etag" in options.basis ? { "if-match": options.basis.etag } : { "if-none-match": "*" }),
          "x-stow-save-id": options.saveId, "content-type": options.contentType ?? "application/octet-stream",
          "x-stow-metadata": JSON.stringify(options.metadata ?? {}),
        },
      });
      if (response.status === 412) throw new FileHistoryError("conflict", "Another writer changed the remote file. Your local stages are retained.");
      if (!response.ok) throw new Error(`Remote write failed (${response.status})`);
      return { etag: response.headers.get("etag") };
    },
  };
}
