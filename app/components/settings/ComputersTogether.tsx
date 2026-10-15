import React, { useCallback, useEffect, useState } from "react";
import * as Haptics from "expo-haptics";
import { ActionMenu, ListRow, ListSection, confirmDestructive } from "../ui";
import type { ActionMenuItem } from "../ui/ActionMenu";
import {
  connectComputers,
  forgetComputer,
  listComputers,
  type ComputerPeer,
} from "../../services/computers";
import { peersForDaemon } from "../../services/computerPeers";
import type { StoredServer } from "../../services/storage";

// One of the user's other computers, as this section shows it: a server on
// this phone, or a computer the current one works with that this phone
// doesn't know.
type OtherComputer = { key: string; name: string; daemonId: string; server: StoredServer | null };

// ComputersTogether lets the computer in use hand work to the user's other
// computers. One tap: this phone, trusted by both, introduces them. A paired
// computer's menu stops working together, on both computers.
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
  const [peers, setPeers] = useState<ComputerPeer[] | null>(null);
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [menuFor, setMenuFor] = useState<OtherComputer | null>(null);
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

  if (!current) return null;
  const others: OtherComputer[] = servers
    .filter((server) => server.id !== currentServerId)
    .map((server) => ({ key: server.id, name: server.name, daemonId: server.daemonId, server }));
  for (const peer of peers ?? []) {
    const daemonId = peer.daemon_id || peer.device_id?.replace(/^computer-/, "") || "";
    if (daemonId && !others.some((other) => peersForDaemon([peer], other.daemonId).length > 0)) {
      others.push({ key: `peer:${daemonId}`, name: peer.name, daemonId, server: null });
    }
  }
  if (others.length === 0) return null;

  const run = async (other: OtherComputer, work: () => Promise<void>) => {
    void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    setBusyKey(other.key);
    setError(null);
    try {
      await work();
      await reload();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setBusyKey(null);
    }
  };
  const connect = (other: OtherComputer) =>
    other.server && run(other, () => connectComputers(current, other.server!));
  const forget = (other: OtherComputer) =>
    confirmDestructive({
      title: `Stop working with ${other.name}?`,
      message: `Neither computer can hand the other work any more, and messages still waiting are dropped. Connect them again here any time.`,
      confirmLabel: "Stop",
      onConfirm: () =>
        run(other, async () => {
          const unreachable = await forgetComputer(current, other.daemonId, other.server);
          if (unreachable) {
            throw new Error(`${current.name} stopped. ${other.name} could not be told (${unreachable}); it can no longer reach ${current.name} anyway.`);
          }
        }),
    });

  const menuItems: ActionMenuItem[] = [];
  if (menuFor) {
    const outbound = peersForDaemon(peers ?? [], menuFor.daemonId).some((peer) => peer.direction === "outbound");
    if (!outbound && menuFor.server) {
      menuItems.push({
        key: "connect",
        icon: "desktop",
        label: `Let ${current.name} hand it work`,
        onPress: () => void connect(menuFor),
      });
    }
    menuItems.push({
      key: "forget",
      icon: "trash",
      label: "Stop working together",
      destructive: true,
      onPress: () => forget(menuFor),
    });
  }

  return (
    <>
      <ListSection
        title="Working together"
        footer={
          error ??
          `${current.name}'s Brain can send a task to another computer, which does it with its own apps, files and logged-in browser.`
        }
      >
        {others.map((other) => {
          const paired = peersForDaemon(peers ?? [], other.daemonId);
          const outbound = paired.find((peer) => peer.direction === "outbound");
          const busy = busyKey === other.key;
          const subtitle = outbound
            ? `${current.name} can hand it work · ${outbound.online ? "online" : "offline"}`
            : paired.length > 0
              ? `It can hand ${current.name} work`
              : other.server
                ? `Tap to let ${current.name} hand it work`
                : "Not on this phone";
          return (
            <ListRow
              key={other.key}
              title={other.name}
              subtitle={subtitle}
              icon="desktop"
              loading={busy}
              disabled={!connected || busy || peers === null}
              accessibilityLabel={`${other.name}, ${subtitle}`}
              accessibilityHint={paired.length > 0 ? "Shows options for working together" : undefined}
              onPress={() => (paired.length > 0 ? setMenuFor(other) : void connect(other))}
            />
          );
        })}
      </ListSection>
      <ActionMenu
        visible={menuFor !== null}
        title={menuFor?.name ?? ""}
        items={menuItems}
        onClose={() => setMenuFor(null)}
      />
    </>
  );
}
