import { type EmbeddedHost } from "./embedded.js";
import { type WorkerEndpoint } from "./worker-protocol.js";
import type { AsyncEmbeddedHost, WorkerHostOptions } from "./worker-host.js";
/** Cancellation stops calls awaiting initialization; started Go operations finish. */
export declare function serveEmbeddedWorker(endpoint: WorkerEndpoint, host: Promise<EmbeddedHost | AsyncEmbeddedHost>, options?: WorkerHostOptions): () => void;
//# sourceMappingURL=worker-server.d.ts.map