/** A lost write reply is acknowledged only after both identity and content match. */
export async function reconcileRemote<T>(
  read: () => Promise<T | null>, matches: (object: T) => boolean,
  acknowledge: (object: T) => Promise<void>,
): Promise<boolean> {
  const observed = await read();
  if (observed === null || !matches(observed)) return false;
  await acknowledge(observed);
  return true;
}
