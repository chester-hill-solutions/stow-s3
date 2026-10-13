import type { EmbeddedBinaryResponse } from "./embedded.js";
export interface WorkerEndpoint {
    postMessage(message: unknown, transfer?: Transferable[]): void;
    addEventListener(type: "message", listener: (event: MessageEvent<unknown>) => void): void;
    removeEventListener(type: "message", listener: (event: MessageEvent<unknown>) => void): void;
}
export interface WorkerRequest {
    type: "stow-call";
    id: number;
    metadata: string;
    data?: Uint8Array;
}
export interface WorkerReply extends EmbeddedBinaryResponse {
    type: "stow-result";
    id: number;
}
export interface WorkerFailure {
    type: "stow-error";
    id: number;
    message: string;
    code?: string;
}
export declare function isWorkerRequest(value: unknown): value is WorkerRequest;
export declare function isWorkerReply(value: unknown): value is WorkerReply | WorkerFailure;
export declare function isRecord(value: unknown): value is Record<string, unknown>;
export declare function isWorkerControl(metadata: string, data?: Uint8Array): boolean;
//# sourceMappingURL=worker-protocol.d.ts.map