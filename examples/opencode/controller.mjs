import { randomUUID } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";

export class SaveFailure extends Error {
  constructor(message, details = {}) {
    super(message);
    this.name = "SaveFailure";
    Object.assign(this, details);
  }
}

export async function saveWithRetry(storage, request, { signal, attempts = 3, random = Math.random, sleep = delay }) {
  if (!Number.isInteger(attempts) || attempts < 1 || attempts > 3) throw new Error("attempts must be 1..3");
  let last;
  for (let attempt = 1; attempt <= attempts; attempt++) {
    signal.throwIfAborted();
    try {
      const resolved = await storage.resolve(request, signal);
      if (resolved.outcome === "committed") return resolved;
      if (resolved.outcome !== "not_found") throw new SaveFailure("Capture outcome is unresolved", { outcome: "unknown" });
      const saved = await storage.capture(request, signal);
      if (saved.outcome !== "committed") throw new SaveFailure("Capture did not confirm publication", { outcome: "unknown" });
      return saved;
    } catch (error) {
      last = error;
      if (signal.aborted) throw error;
      if (error.retryable && error.outcome !== "unknown") {
        if (attempt === attempts) throw new SaveFailure(error.message, { cause: error, outcome: error.outcome, attempts: attempt });
        await sleep(random() * Math.min(250 * 2 ** (attempt - 1), 2000), undefined, { signal });
        continue;
      }
      // A reply can be lost after commit. Reconciliation never runs the prompt again.
      try {
        const resolved = await storage.resolve(request, signal);
        if (resolved.outcome === "committed") return resolved;
        if (resolved.outcome !== "not_found") throw new SaveFailure("Capture outcome is unresolved", { outcome: "unknown" });
      } catch (resolveError) {
        throw new SaveFailure("Could not establish capture outcome", { outcome: "unknown", cause: resolveError, attempts: attempt });
      }
      if (!error.retryable || attempt === attempts) throw new SaveFailure(error.message, { cause: error, outcome: error.outcome, attempts: attempt });
      await sleep(random() * Math.min(250 * 2 ** (attempt - 1), 2000), undefined, { signal });
    }
  }
  throw last;
}

export class TurnController {
  #busy = false;
  constructor({ store, agent, storage, quiesce, writeProgress, deadlineMs }) {
    if (!Number.isFinite(deadlineMs) || deadlineMs <= 0) throw new Error("a positive save deadline is required");
    Object.assign(this, { store, agent, storage, quiesce, writeProgress, deadlineMs });
  }

  async turn(prompt, { signal = new AbortController().signal } = {}) {
    if (this.#busy) throw new Error("another turn owns admission");
    this.#busy = true;
    try {
      const previous = await this.store.load();
      if (previous && previous.phase !== "saved") throw new Error("previous turn needs explicit recovery; admission is closed");
      if (previous?.terminal !== "succeeded") {
        if (previous) throw new Error("previous execution failed; explicit caller decision required");
      }
      const intent = { version: 1, phase: "running", prompt, turnID: randomUUID(), lastCheckpointID: previous?.lastCheckpointID };
      await this.store.save(intent);
      const terminal = await this.agent.execute(prompt, intent.turnID, signal);
      if (!["succeeded", "failed", "interrupted"].includes(terminal.outcome)) throw new Error("missing terminal execution outcome");
      await this.quiesce(signal);
      await this.writeProgress({ prompt, terminal, previous: intent.lastCheckpointID });
      const request = { key: randomUUID(), parent: intent.lastCheckpointID };
      const pending = { ...intent, phase: "saving", terminal: terminal.outcome, sessionID: terminal.sessionID, messageID: terminal.messageID, request };
      await this.store.save(pending);
      const deadline = AbortSignal.any([signal, AbortSignal.timeout(this.deadlineMs)]);
      const saved = await saveWithRetry(this.storage, request, { signal: deadline });
      const complete = { ...pending, phase: "saved", lastCheckpointID: saved.checkpoint.id };
      await this.store.save(complete);
      return complete;
    } finally {
      this.#busy = false;
    }
  }

  async recover({ signal = new AbortController().signal } = {}) {
    if (this.#busy) throw new Error("another turn owns admission");
    this.#busy = true;
    try {
      const pending = await this.store.load();
      if (!pending || pending.phase !== "saving") throw new Error("no reconcilable capture; prompt execution will not be replayed");
      const deadline = AbortSignal.any([signal, AbortSignal.timeout(this.deadlineMs)]);
      const saved = await this.storage.resolve(pending.request, deadline);
      if (saved.outcome !== "committed") throw new SaveFailure("Recovery did not find a confirmed save; explicit new capture decision required", { outcome: saved.outcome });
      const complete = { ...pending, phase: "saved", lastCheckpointID: saved.checkpoint.id };
      await this.store.save(complete);
      return complete;
    } finally { this.#busy = false; }
  }
}
