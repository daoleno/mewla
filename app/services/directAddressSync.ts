import { directCandidate, learnDirectAddresses } from "./directAddresses";
import * as Storage from "./storage";

/**
 * Keeps the direct addresses a computer announced after it connects, on the
 * saved server so a cold start can try them. The computer's latest list
 * replaces the old one; an empty list removes them.
 */
export async function rememberDirectAddresses(
  serverId: string,
  payload: unknown,
): Promise<void> {
  const server = await Storage.getServerById(serverId);
  if (!server || server.transportKind !== "link") return;
  const { addresses, changed } = learnDirectAddresses(server, payload);
  if (!changed) return;
  const path = new URL(server.url).pathname;
  await Storage.updateServer(server.id, {
    transportCandidates: [
      ...(server.transportCandidates ?? []).filter((candidate) => candidate.kind !== "direct"),
      ...addresses.map((address) => directCandidate(address, path)),
    ],
  });
}
