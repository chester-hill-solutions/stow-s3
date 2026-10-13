export interface OperationQueues {
    tail: Promise<void>;
    syncTail: Promise<void>;
}
export declare function operationQueues(owner: object, key: string): OperationQueues;
export declare function enqueueOperation<T>(queue: OperationQueues, slot: "tail" | "syncTail", operation: () => Promise<T>): Promise<T>;
//# sourceMappingURL=operation-queue.d.ts.map