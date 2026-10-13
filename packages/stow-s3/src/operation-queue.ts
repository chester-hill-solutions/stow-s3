export interface OperationQueues { tail: Promise<void>; syncTail: Promise<void> }
const owners = new WeakMap<object, Map<string, OperationQueues>>();
export function operationQueues(owner: object, key: string): OperationQueues {
  let queues = owners.get(owner);
  if (!queues) { queues = new Map(); owners.set(owner, queues); }
  let queue = queues.get(key);
  if (!queue) { queue = { tail: Promise.resolve(), syncTail: Promise.resolve() }; queues.set(key, queue); }
  return queue;
}
export function enqueueOperation<T>(queue: OperationQueues, slot: "tail" | "syncTail", operation: () => Promise<T>): Promise<T> {
  const result = queue[slot].then(operation);
  queue[slot] = result.then(() => undefined, () => undefined);
  return result;
}
