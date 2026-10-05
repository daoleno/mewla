import React from "react";
import { useLocalSearchParams, useRouter } from "expo-router";
import { AccountsView } from "../../../components/plugins/PluginConnectionViews";
import { pluginServicePath, PluginsPage, usePluginService, usePluginsFlow } from "../../../components/plugins/PluginsFlow";

export default function PluginAccountsScreen() {
  const { service } = useLocalSearchParams<{ service: string }>();
  const router = useRouter();
  const flow = usePluginsFlow();
  usePluginService(service);
  const selected = flow.selected?.id === service ? flow.selected : null;
  const toService = () => router.dismissTo(pluginServicePath(service));
  return <PluginsPage title="Connected accounts">
    {selected ? <AccountsView
      plugin={selected}
      accounts={flow.accounts.filter((a) => a.status !== "disconnected" && a.integration === selected.id)}
      selectedId={flow.account?.id ?? null}
      busy={flow.busy}
      onOpen={(item) => { void flow.send({ action: "get", id: item.id }).then((response) => { if (response) toService(); }); }}
      onConnectAnother={() => { flow.reconnect(); toService(); }}
      onDisconnect={(item) => flow.disconnect(item, toService)}
    /> : null}
  </PluginsPage>;
}
