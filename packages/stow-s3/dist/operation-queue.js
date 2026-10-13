const owners = new WeakMap();
export function operationQueues(owner, key) {
    let queues = owners.get(owner);
    if (!queues) {
        queues = new Map();
        owners.set(owner, queues);
    }
    let queue = queues.get(key);
    if (!queue) {
        queue = { tail: Promise.resolve(), syncTail: Promise.resolve() };
        queues.set(key, queue);
    }
    return queue;
}
export function enqueueOperation(queue, slot, operation) {
    const result = queue[slot].then(operation);
    queue[slot] = result.then(() => undefined, () => undefined);
    return result;
}
//# sourceMappingURL=operation-queue.js.map