import type { ComputerPeer } from "./computers";

// peersForDaemon finds a computer's entries, in either direction: an inbound
// one may know only the start of its daemon id, from its device id.
export function peersForDaemon(peers: readonly ComputerPeer[], daemonId: string): ComputerPeer[] {
  const id = daemonId.toLowerCase();
  if (!id) return [];
  return peers.filter((peer) => {
    if (peer.daemon_id) return peer.daemon_id.toLowerCase() === id;
    const prefix = peer.device_id?.replace(/^computer-/, "").toLowerCase();
    return Boolean(prefix && prefix.length >= 16 && id.startsWith(prefix));
  });
}
