/** A lost write reply is acknowledged only after both identity and content match. */
export declare function reconcileRemote<T>(read: () => Promise<T | null>, matches: (object: T) => boolean, acknowledge: (object: T) => Promise<void>): Promise<boolean>;
//# sourceMappingURL=remote-reconciliation.d.ts.map