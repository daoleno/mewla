import React, { useCallback, useEffect, useState } from "react";
import * as Haptics from "expo-haptics";
import { ListRow, ListSection } from "../ui";
import { connectComputers, listComputers, type ComputerPeer } from "../../services/computers";
import type { StoredServer } from "../../services/storage";

// ComputersTogether lets the computer in use hand work to the user's other
// computers. One tap: this phone, trusted by both, introduces them.
export function ComputersTogether({
  servers,
  currentServerId,
  connected,
}: {
  servers: StoredServer[];
  currentServerId: string | null;
  connected: boolean;
}) {
  const current = servers.find((server) => server.id === currentServerId) ?? null;
  const others = servers.filter((server) => server.id !== currentServerId);
  const [peers, setPeers] = useState<ComputerPeer[] | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    if (!current || !connected) return;
    try {
      setPeers(await listComputers(current));
      setError(null);
    } catch (reason) {
      setPeers(null);
      setError(reason instanceof Error ? reason.message : String(reason));
    }
  }, [current, connected]);

  useEffect(() => {
    setPeers(null);
    void reload();
  }, [reload]);

  if (!current || others.length === 0) return null;

  return (
    <ListSection
      title="Working together"
      footer={
        error ??
        `${current.name}'s Brain can send a task to another computer, which does it with its own apps, files and logged-in browser.`
      }
    >
      {others.map((other) => {
        const peer = peers?.find(
          (candidate) => candidate.direction === "outbound" && candidate.daemon_id === other.daemonId,
        );
        const busy = busyId === other.id;
        const subtitle = peer
          ? `${current.name} can hand it work · ${peer.online ? "online" : "offline"}`
          : `Tap to let ${current.name} hand it work`;
        return (
          <ListRow
            key={other.id}
            title={other.name}
            subtitle={subtitle}
            icon="desktop"
            loading={busy}
            disabled={!connected || busy || Boolean(peer) || peers === null}
            accessibilityLabel={`${other.name}, ${subtitle}`}
            onPress={async () => {
              void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
              setBusyId(other.id);
              setError(null);
              try {
                await connectComputers(current, other);
                await reload();
              } catch (reason) {
                setError(reason instanceof Error ? reason.message : String(reason));
              } finally {
                setBusyId(null);
              }
            }}
          />
        );
      })}
    </ListSection>
  );
}
