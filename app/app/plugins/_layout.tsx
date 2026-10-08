import React, { useCallback, useEffect, useState } from "react";
import { Linking } from "react-native";
import { Stack } from "expo-router";
import { useCurrentServer } from "../../store/currentServer";
import { useWorkers } from "../../store/workers";
import { useStackScreenOptions } from "../../components/navigation/stackScreenOptions";
import { PluginsFlowProvider } from "../../components/plugins/PluginsFlow";
import type { ServerConnection } from "../../components/extensions/ServerOfflineNotice";
import { secureStorage } from "../../services/secureStorage";
import { retainPluginReturn } from "../../services/pluginOnboarding";

// A deep-linked service page still has the catalog below it.
export const unstable_settings = { initialRouteName: "index" };

export default function PluginsLayout() {
  const { currentServerId, currentServer, servers, isCurrentServer } = useCurrentServer();
  const { state } = useWorkers();
  const screenOptions = useStackScreenOptions();
  const connection: ServerConnection = currentServerId ? state.serverConnections[currentServerId] ?? "offline" : "offline";
  const [deferredServerId, setDeferredServerId] = useState<string | null>(null);
  useEffect(() => { if (deferredServerId === currentServerId) setDeferredServerId(null); }, [currentServerId, deferredServerId]);
  const deferReturn = useCallback(async (url: string) => {
    const owner = await retainPluginReturn(servers.filter((s) => !isCurrentServer(s.id)).map((s) => s.id), url, secureStorage);
    if (owner) setDeferredServerId(owner);
  }, [servers, isCurrentServer]);
  useEffect(() => {
    const receive = (url: string) => { void deferReturn(url).catch(() => undefined); };
    void Linking.getInitialURL().then((url) => { if (url) receive(url); });
    const listener = Linking.addEventListener("url", ({ url }) => receive(url));
    return () => listener.remove();
  }, [deferReturn]);
  const deferredName = deferredServerId && deferredServerId !== currentServerId ? servers.find((s) => s.id === deferredServerId)?.name : undefined;
  return <PluginsFlowProvider
    key={currentServerId ?? "none"}
    serverId={currentServerId}
    serverName={currentServer?.name ?? "this server"}
    connection={connection}
    deferredName={deferredName}
    deferReturn={deferReturn}
  >
    <Stack screenOptions={screenOptions} />
  </PluginsFlowProvider>;
}
