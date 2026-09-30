import { watch } from "node:fs";
import { mkdir, readFile, readdir, realpath } from "node:fs/promises";
import { join, relative, isAbsolute, sep } from "node:path";
import { randomUUID } from "node:crypto";
import { atomicJSON } from "../opencode/local-store.mjs";

function identifier(value) {
  if (typeof value !== "string" || !/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$/.test(value)) throw new Error("invalid participant or message id");
  return value;
}

function paths(values, patterns = false) {
  if (!Array.isArray(values) || values.length > 200) throw new Error("expected at most 200 relative paths");
  return [...new Set(values.map(value => {
    if (typeof value !== "string" || !value || value.length > 512 || /[\\\x00-\x1f:*?]/.test(value.replace(patterns ? /\/\*\*$/ : /$^/, ""))) throw new Error("invalid relative path");
    const plain = value.replace(patterns ? /\/\*\*$/ : /$^/, "").replace(/\/$/, "");
    if (!plain || plain.startsWith("/") || plain.split("/").some(part => !part || part === "." || part === "..")) throw new Error("paths must be normalized and relative");
    if (!patterns && value.endsWith("/")) throw new Error("publication files must name files");
    return value;
  }))].sort();
}

function matches(pattern, file) {
  if (pattern.endsWith("/**")) pattern = pattern.slice(0, -2);
  return pattern.endsWith("/") ? file.startsWith(pattern) : pattern === file;
}

function overlaps(left, right) {
  const a = left.replace(/\/\*\*$/, "").replace(/\/$/, "");
  const b = right.replace(/\/\*\*$/, "").replace(/\/$/, "");
  return a === b || a.startsWith(`${b}/`) || b.startsWith(`${a}/`);
}

const copy = value => JSON.parse(JSON.stringify(value));
const payload = value => JSON.stringify({ from: value.from, files: value.files, summary: value.summary });

/** A cooperative room for one coordinator process; it does not lock filesystem edits. */
export class CollaborationRoom {
  static async open({ root, stateDir }) {
    await mkdir(root, { recursive: true });
    await mkdir(stateDir, { recursive: true, mode: 0o700 });
    root = await realpath(root);
    stateDir = await realpath(stateDir);
    const rel = relative(root, stateDir);
    if (!rel || (rel !== ".." && !rel.startsWith(`..${sep}`) && !isAbsolute(rel))) throw new Error("room stateDir must be outside the workspace");
    const identityPath = join(stateDir, "room.json");
    let identity;
    try { identity = JSON.parse(await readFile(identityPath, "utf8")); }
    catch (error) { if (error.code !== "ENOENT") throw error; }
    if (identity && identity.root !== root) throw new Error("room stateDir belongs to a different workspace");
    if (!identity) await atomicJSON(identityPath, { root });
    const room = new CollaborationRoom(root, stateDir);
    await mkdir(room.messageDir, { recursive: true });
    await room._reconcile();
    room.watcher = watch(room.messageDir, () => {
      room._serial(() => room._reconcile()).catch(error => room._error(error));
    });
    room.watcher.on("error", error => room._error(error));
    return room;
  }

  constructor(root, stateDir) {
    this.root = root;
    this.stateDir = stateDir;
    this.messageDir = join(root, "collaboration", "messages");
    this.participants = new Map();
    this.claims = new Map();
    this.messages = new Map();
    this.acks = new Map();
    this.listeners = new Map();
    this.sequence = 0;
    this.queue = Promise.resolve();
    this.errors = [];
    this.closed = false;
  }

  _serial(operation) {
    const result = this.queue.then(() => {
      if (this.closed) throw new Error("room is closed");
      return operation();
    });
    this.queue = result.catch(() => {});
    return result;
  }

  _error(error) { this.errors.push(String(error.message || error)); this.errors = this.errors.slice(-10); }
  _participant(id) {
    identifier(id);
    const participant = this.participants.get(id);
    if (!participant || participant.status !== "active") throw new Error(`unknown or inactive participant: ${id}`);
    return participant;
  }

  async _loadAcks(id) {
    if (this.acks.has(id)) return;
    let stored = { acknowledged: [] };
    try { stored = JSON.parse(await readFile(join(this.stateDir, `${id}.json`), "utf8")); }
    catch (error) { if (error.code !== "ENOENT") throw error; }
    if (!Array.isArray(stored.acknowledged)) throw new Error("invalid acknowledgment state");
    this.acks.set(id, new Set(stored.acknowledged.map(identifier)));
  }

  async _reconcile() {
    const sequences = new Set();
    for (const name of (await readdir(this.messageDir)).sort()) {
      if (!name.endsWith(".json")) continue;
      const message = JSON.parse(await readFile(join(this.messageDir, name), "utf8"));
      identifier(message.id); identifier(message.from);
      if (name !== `${message.id}.json`) throw new Error("message filename does not match id");
      message.files = paths(message.files);
      if (typeof message.summary !== "string" || !message.summary.trim() || message.summary.length > 16000 || !Number.isSafeInteger(message.sequence) || message.sequence < 1 || !Array.isArray(message.recipients)) throw new Error("invalid stored message");
      message.recipients.forEach(identifier);
      if (sequences.has(message.sequence)) throw new Error("duplicate message sequence");
      sequences.add(message.sequence);
      const existing = this.messages.get(message.id);
      if (existing && JSON.stringify(existing) !== JSON.stringify(message)) throw new Error("immutable publication was changed");
      this.messages.set(message.id, message);
      this.sequence = Math.max(this.sequence, message.sequence);
    }
    this._notify();
  }

  async register({ id, name = id, ownedFiles = [], subscriptions = [] }) {
    return this._serial(async () => {
      identifier(id);
      if (typeof name !== "string" || !name.trim() || name.length > 160) throw new Error("invalid participant name");
      const participant = { id, name, ownedFiles: paths(ownedFiles, true), subscriptions: paths(subscriptions, true), status: "active" };
      await this._loadAcks(id);
      this.participants.set(id, participant);
      await this._reconcile();
      return copy(participant);
    });
  }

  async claim(id, requestedPaths) {
    return this._serial(async () => {
      this._participant(id);
      const requested = paths(requestedPaths, true);
      for (const [other, claim] of this.claims) {
        if (other !== id && requested.some(path => claim.paths.some(owned => overlaps(path, owned)))) throw new Error(`file ownership conflicts with ${other}`);
      }
      const claim = { id, paths: [...new Set([...(this.claims.get(id)?.paths || []), ...requested])].sort() };
      this.claims.set(id, claim);
      return copy(claim);
    });
  }

  async release(id) {
    return this._serial(async () => {
      this._participant(id);
      this.claims.delete(id);
    });
  }

  async leave(id) {
    return this._serial(async () => {
      this._participant(id);
      this.claims.delete(id);
      this.participants.get(id).status = "stopped";
      for (const listener of this.listeners.get(id) || []) listener.active = false;
      this.listeners.delete(id);
    });
  }

  async publish({ id = randomUUID(), from, files, summary }) {
    return this._serial(async () => {
      identifier(id); this._participant(from);
      files = paths(files);
      if (typeof summary !== "string" || !summary.trim() || summary.length > 16000) throw new Error("summary must contain 1–16000 characters");
      await this._reconcile();
      const proposed = { from, files, summary };
      const existing = this.messages.get(id);
      if (existing) {
        if (payload(existing) !== payload(proposed)) throw new Error("message id reused with different content");
        return copy(existing);
      }
      const recipients = [...this.participants.values()].filter(participant => participant.status === "active" && participant.id !== from && files.some(file => participant.subscriptions.some(pattern => matches(pattern, file)))).map(participant => participant.id).sort();
      const message = { id, sequence: this.sequence + 1, from, files, summary, recipients, createdAt: new Date().toISOString() };
      await atomicJSON(join(this.messageDir, `${id}.json`), message);
      this.sequence = message.sequence;
      this.messages.set(id, message);
      this._notify();
      return copy(message);
    });
  }

  _pending(id) {
    return [...this.messages.values()].filter(message => message.recipients.includes(id) && !this.acks.get(id)?.has(message.id)).sort((a, b) => a.sequence - b.sequence);
  }

  async pending(id) {
    return this._serial(async () => {
      this._participant(id);
      await this._reconcile();
      return copy(this._pending(id));
    });
  }

  async acknowledge(id, messageIDs) {
    return this._serial(async () => {
      this._participant(id);
      if (!Array.isArray(messageIDs) || messageIDs.length > 1000) throw new Error("invalid acknowledgment ids");
      await this._reconcile();
      for (const messageID of messageIDs) {
        identifier(messageID);
        if (!this.messages.get(messageID)?.recipients.includes(id)) throw new Error("cannot acknowledge a message not addressed to participant");
      }
      const acknowledged = new Set([...this.acks.get(id), ...messageIDs]);
      await atomicJSON(join(this.stateDir, `${id}.json`), { acknowledged: [...acknowledged].sort() });
      this.acks.set(id, acknowledged);
    });
  }

  listen(id, callback) {
    this._participant(id);
    if (typeof callback !== "function") throw new Error("listener requires a callback");
    const listener = { callback, seen: new Set(), active: true };
    if (!this.listeners.has(id)) this.listeners.set(id, new Set());
    this.listeners.get(id).add(listener);
    this._serial(() => this._reconcile()).catch(error => this._error(error));
    return () => { listener.active = false; this.listeners.get(id)?.delete(listener); };
  }

  _notify() {
    for (const [id, listeners] of this.listeners) for (const listener of listeners) {
      for (const message of this._pending(id)) {
        if (listener.seen.has(message.id)) continue;
        listener.seen.add(message.id);
        queueMicrotask(() => {
          if (!listener.active || this.closed) return;
          try { Promise.resolve(listener.callback(copy(message))).catch(error => this._error(error)); }
          catch (error) { this._error(error); }
        });
      }
    }
  }

  snapshot() {
    return copy({ participants: [...this.participants.values()], claims: [...this.claims.values()], messages: [...this.messages.values()].sort((a, b) => a.sequence - b.sequence), deliveries: [...this.participants.keys()].map(id => ({ id, pending: this._pending(id).map(message => message.id), acknowledged: [...(this.acks.get(id) || [])] })), errors: this.errors });
  }

  async exportState() {
    return this._serial(async () => {
      await this._reconcile();
      const { participants, messages } = this.snapshot();
      const state = { version: 1, participants: participants.map(({ id, name, ownedFiles, subscriptions }) => ({ id, name, ownedFiles, subscriptions })), messages };
      await atomicJSON(join(this.root, "collaboration", "state.json"), state);
      return state;
    });
  }

  close() {
    this.closed = true;
    this.watcher?.close();
    for (const listeners of this.listeners.values()) for (const listener of listeners) listener.active = false;
    this.listeners.clear();
  }
}
