export async function shutdownPilot({ server, release, holder }, failure) {
  if (failure?.agentStopUnconfirmed) throw failure;
  // Retain both claims if the agent may still be writing.
  if (server) await server.close();
  await holder.close();
  if (release) await release();
}
