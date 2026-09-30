import { createHash, randomUUID } from "node:crypto";
import { constants } from "node:fs";
import { mkdir, open, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { OpenCodeAgent } from "../opencode/opencode.mjs";
import { startOpenCode } from "../opencode/server.mjs";
import { atomicJSON, jsonStore } from "../opencode/local-store.mjs";
import { workspaceChanges } from "../opencode/workspace-files.mjs";

export function participantPermissions(files) {
  for (const file of files) {
    if (!/^[a-zA-Z0-9_./-]+$/.test(file) || file.startsWith("/") || file.split("/").some(part => !part || part === "." || part === "..")) throw new Error("writable files must be exact workspace-relative paths");
  }
  return [{ action: "*", resource: "*", effect: "deny" },
    ...["read", "glob", "grep"].map(action => ({ action, resource: "*", effect: "allow" })),
    ...files.map(resource => ({ action: "edit", resource, effect: "allow" }))];
}

async function fileState(root, files) {
  const result = {};
  for (const name of files) {
    let file;
    try { file = await open(join(root, name), constants.O_RDONLY | constants.O_NOFOLLOW); }
    catch (error) { if (error.code === "ENOENT") continue; throw error; }
    try {
      const info = await file.stat();
      if (!info.isFile() || info.size > 1 << 20) throw new Error("participant output must be a regular file under 1 MiB");
      const bytes = await file.readFile();
      if (bytes.length > 1 << 20) throw new Error("participant output exceeds 1 MiB");
      result[name] = createHash("sha256").update(bytes).digest("hex");
    } finally { await file.close(); }
  }
  return result;
}

/** Prototype: one owner coordinates trusted, file-only OpenCode participants. */
export class MultiplayerCoordinator {
  constructor({ room, root, stateDir, profile, simulate = false, signal }) {
    Object.assign(this, { room, root, stateDir, profile, simulate, signal });
    this.participants = new Map();
    this.deliveries = [];
    this.activity = [];
    this.interruptions = 0;
  }

  async register(spec) {
    if (this.stopping) throw new Error("coordinator is stopping");
    if (this.participants.has(spec.id)) throw new Error("participant already registered");
    const notesPath = `collaboration/notes/${spec.id}.md`;
    const participant = { ...spec, notesPath, ownedFiles: [...spec.files, notesPath], status: "ready", turns: 0, received: [], generation: 0 };
    await mkdir(join(this.root, "collaboration", "notes"), { recursive: true });
    await mkdir(join(this.root, "collaboration", "progress"), { recursive: true });
    await this.room.register(participant);
    participant.unsubscribe = this.room.listen(spec.id, message => {
      if (!participant.received.includes(message.id)) {
        participant.received.push(message.id);
        this.deliveries.push({ messageID: message.id, to: spec.id, status: "queued", at: new Date().toISOString() });
      }
    });
    this.participants.set(spec.id, participant);
    return participant;
  }

  async start(id) {
    const p = this.participants.get(id);
    if (this.simulate || p.server) return;
    p.status = "starting";
    p.interruptRequested = false;
    const stateDir = join(this.stateDir, id, `generation-${++p.generation}`);
    for (const [source, leaf] of [[this.profile.providerConfigPath, ".config/opencode/opencode.jsonc"], [this.profile.modelCatalogPath, ".cache/opencode/models.json"]]) {
      if (!source) continue;
      const path = join(stateDir, "opencode", "home", leaf);
      await mkdir(dirname(path), { recursive: true, mode: 0o700 });
      const file = await open(path, "wx", 0o600);
      try { await file.writeFile(await readFile(source)); } finally { await file.close(); }
    }
    p.server = await startOpenCode({ binary: this.profile.opencodeBinary, stateDir, directory: this.root, signal: this.signal });
    p.agent = new OpenCodeAgent({ endpoint: p.server.endpoint, password: p.server.password, model: this.profile.model,
      directory: this.root, title: `Stow multiplayer / ${p.name}`, notesPath: p.notesPath,
      progressPath: `collaboration/progress/${id}.json`, permissions: participantPermissions(p.ownedFiles),
      sessionStore: jsonStore(join(stateDir, "agent.json"), `${id}:${this.profile.model.providerID}/${this.profile.model.id}`),
      onAdmitted: admission => {
        p.sessionID = admission.sessionID;
        p.admitted = true;
        this.activity.push({ participant: id, turn: p.turns, event: "admitted", at: Date.now(), sessionID: admission.sessionID });
        p.admissionResolve?.(admission);
      },
    });
    p.status = "ready";
  }

  async turn(id, prompt, { simulateAction, messages = [] } = {}) {
    if (this.stopping) throw new Error("coordinator is stopping");
    const p = this.participants.get(id);
    if (!p) throw new Error("unknown participant");
    if (p.busy) throw new Error("participant already has an active turn");
    p.busy = true;
    let settled = false;
    let stopUnconfirmed = false;
    try {
      await this.room.claim(id, p.ownedFiles);
      await this.start(id);
      const before = await fileState(this.root, p.files);
      p.turns++;
      p.status = "running";
      p.admitted = false;
      p.admission = new Promise(resolve => { p.admissionResolve = resolve; });
      p.turnAbort = new AbortController();
      const signal = AbortSignal.any([this.signal ?? new AbortController().signal, p.turnAbort.signal, AbortSignal.timeout(180000)]);
      this.activity.push({ participant: id, turn: p.turns, event: "started", at: Date.now() });
      await atomicJSON(join(this.root, "collaboration", "progress", `${id}.json`), { participant: id, turn: p.turns, status: "running", messages: messages.map(m => m.id) });
      const context = messages.length ? `\nRelevant subscribed reports:\n${messages.map(m => `Read collaboration/messages/${m.id}.json. From ${m.from}; files ${m.files.join(", ")}. Explanation: ${m.summary}`).join("\n")}` : "";
      let terminal;
      if (this.simulate) {
        p.admitted = true; p.admissionResolve({ sessionID: "simulated" });
        this.activity.push({ participant: id, turn: p.turns, event: "admitted", at: Date.now() });
        await simulateAction(signal);
        terminal = { outcome: "succeeded", sessionID: "simulated" };
      } else {
        terminal = await p.agent.execute(`${prompt}${context}\nYou own ONLY these writable files: ${p.ownedFiles.join(", ")}. Keep this small prototype focused: write the requested files promptly, avoid extra analysis or features. Explain your actual changes and implications in at most 150 words in your notes. Read no other participant's notes or reports unless delivered in the subscribed context above.`, randomUUID(), signal);
        await p.agent.quiesce(signal);
      }
      settled = true;
      if (terminal.outcome !== "succeeded") throw new Error(`${id} terminal outcome: ${terminal.outcome}`);
      const changes = workspaceChanges(before, await fileState(this.root, p.files));
      const files = [...changes.added, ...changes.modified, ...changes.deleted].sort();
      if (!files.length) throw new Error(`${id} succeeded without changing its assigned task outputs`);
      const summary = (await readFile(join(this.root, p.notesPath), "utf8")).trim().slice(0, 16000);
      const message = await this.room.publish({ from: id, files, summary });
      p.status = "idle";
      for (const delivery of this.deliveries) if (delivery.to === id && messages.some(m => m.id === delivery.messageID)) delivery.status = "applied";
      await atomicJSON(join(this.root, "collaboration", "progress", `${id}.json`), { participant: id, turn: p.turns, status: "succeeded", sessionID: terminal.sessionID, changes, appliedMessages: messages.map(m => m.id), publication: message.id });
      this.activity.push({ participant: id, turn: p.turns, event: "idle", at: Date.now() });
      return { terminal, changes, message };
    } catch (error) {
      if (error.agentStopUnconfirmed) { stopUnconfirmed = true; p.status = "stop-unconfirmed"; throw error; }
      if (!settled && p.server) {
        try { await p.server.close(); p.server = undefined; settled = true; }
        catch (stopError) { stopUnconfirmed = true; p.status = "stop-unconfirmed"; throw Object.assign(new Error(`${id} writer stop unconfirmed`, { cause: stopError }), { agentStopUnconfirmed: true }); }
      }
      p.status = p.interruptRequested ? "interrupted" : "failed";
      p.error = error.message;
      this.activity.push({ participant: id, turn: p.turns, event: p.status, at: Date.now() });
      throw error;
    } finally {
      try {
        if (!stopUnconfirmed && (settled || this.simulate || !p.server)) await this.room.release(id);
      } finally { p.busy = false; }
    }
  }

  async interrupt(id) {
    const p = this.participants.get(id);
    if (!p.admitted || p.status !== "running") throw new Error("interruption requires an admitted running turn");
    p.interruptRequested = true;
    p.turnAbort.abort(new Error("controlled participant interruption"));
    this.interruptions++;
  }

  async stopWriters() {
    this.stopping = true;
    const outcomes = await Promise.allSettled([...this.participants.values()].map(async p => {
      if (p.busy || p.status === "running" || p.status === "starting") throw new Error(`${p.id} still owns a running turn`);
      if (p.server) { await p.server.close(); p.server = undefined; }
      p.status = "stopped";
    }));
    if (outcomes.some(result => result.status === "rejected")) throw Object.assign(new Error("some participant stops unconfirmed; workspace ownership retained"), { agentStopUnconfirmed: true });
  }

  snapshot() {
    const room = this.room.snapshot();
    return { ...room, participants: [...this.participants.values()].map(p => ({ id: p.id, name: p.name, role: p.role,
      status: p.status, ownedFiles: p.ownedFiles, subscriptions: p.subscriptions, turns: p.turns, received: p.received, error: p.error })),
      claims: room.claims.map(c => ({ owner: c.id, files: c.paths })), deliveries: this.deliveries,
      metrics: { turns: [...this.participants.values()].reduce((sum, p) => sum + p.turns, 0),
        modelTurns: this.simulate ? 0 : [...this.participants.values()].reduce((sum, p) => sum + p.turns, 0), reports: room.messages.length,
        deliveredReports: this.deliveries.length, potentialBroadcastDeliveries: room.messages.length * Math.max(0, this.participants.size - 1),
        listenerModelPolls: 0, interruptions: this.interruptions }, activity: this.activity };
  }
}
