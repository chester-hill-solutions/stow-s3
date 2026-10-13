/** A lost write reply is acknowledged only after both identity and content match. */
export async function reconcileRemote(read, matches, acknowledge) {
    const observed = await read();
    if (observed === null || !matches(observed))
        return false;
    await acknowledge(observed);
    return true;
}
//# sourceMappingURL=remote-reconciliation.js.map