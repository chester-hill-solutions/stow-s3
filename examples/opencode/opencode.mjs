export class OpenCodeAgent {
  constructor({ endpoint, password, model, directory, sessionStore, fetchImpl = fetch }) {
    const url = new URL(endpoint);
    if (!["127.0.0.1", "[::1]"].includes(url.hostname) || url.protocol !== "http:" || url.username || url.password) throw new Error("pilot requires a dedicated loopback OpenCode endpoint");
    Object.assign(this, { endpoint: url.origin, password, model, directory, sessionStore, fetchImpl });
  }

  async request(path, method, body, signal) {
    const response = await this.fetchImpl(this.endpoint + path, {
      method, signal, redirect: "error",
      headers: { "content-type": "application/json", authorization: `Basic ${Buffer.from(`opencode:${this.password}`).toString("base64")}` },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!response.ok) throw new Error(`OpenCode ${method} ${path} returned ${response.status}`);
    if (response.status === 204) return undefined;
    const text = await response.text();
    if (text.length > 4 * 1024 * 1024) throw new Error("OpenCode response exceeds pilot limit");
    return text ? JSON.parse(text) : undefined;
  }

  async session(signal) {
    const existing = await this.sessionStore.load();
    if (existing) return existing.id;
    const created = await this.request("/api/session", "POST", {
      title: "Stow checkpoint pilot", location: { directory: this.directory }, model: this.model,
      permissions: [{ action: "*", resource: "*", effect: "deny" },
        ...["read", "edit", "write", "glob", "grep"].map(action => ({ action, resource: "*", effect: "allow" }))],
    }, signal);
    if (typeof created?.data?.id !== "string") throw new Error("OpenCode did not create a session");
    await this.sessionStore.save({ id: created.data.id });
    return created.data.id;
  }

  async execute(prompt, turnID, signal) {
    const sessionID = await this.session(signal);
    const messageID = `msg_${turnID.replaceAll("-", "")}`;
    const admitted = await this.request(`/api/session/${sessionID}/prompt`, "POST", {
      id: messageID,
      text: `${prompt}\n\nRead STOW_NOTES.md if present. Before finishing, write progress and remaining work to STOW_NOTES.md. Use file tools only; do not start processes, background work or subagents.`,
    }, signal);
    if (admitted?.data?.id !== messageID) throw new Error("OpenCode admission identity mismatch");
    await this.request(`/api/experimental/session/${sessionID}/wait`, "POST", undefined, signal);
    const terminal = await this.request(`/api/session/${sessionID}`, "GET", undefined, signal);
    const value = terminal?.data;
    if (!["succeeded", "failed", "interrupted"].includes(value?.outcome) || !(value.time?.idle >= admitted.data.time?.created)) throw new Error("OpenCode has no terminal outcome for this admission");
    return { outcome: value.outcome, sessionID, messageID, idle: value.time.idle };
  }

  async quiesce(signal) {
    const session = await this.sessionStore.load();
    if (!session) throw new Error("no session to quiesce");
    await this.request(`/api/experimental/session/${session.id}/wait`, "POST", undefined, signal);
    const context = await this.request(`/api/session/${session.id}/context`, "GET", undefined, signal);
    rejectProcessTools(context);
  }
}

export function rejectProcessTools(value) {
  if (!value || typeof value !== "object") return;
  // Refuse the save if the selected profile encountered execution-capable tools.
  const tool = value.type === "tool" ? value.name : value.tool ?? value.toolName;
  if (value.type === "shell" || value.type === "subagent") throw new Error("unsupported process writer in file-only pilot");
  if (value.type === "tool" && !["completed", "error"].includes(value.state?.status)) throw new Error("tool writer has not settled");
  if (typeof tool === "string" && !["read", "edit", "write", "glob", "grep"].includes(tool)) throw new Error(`unsupported writer/tool in file-only pilot: ${tool}`);
  for (const child of Object.values(value)) rejectProcessTools(child);
}
