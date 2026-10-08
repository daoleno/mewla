import React from "react";
import { useRouter } from "expo-router";
import { CatalogView } from "../../components/plugins/PluginConnectionViews";
import { pluginServicePath, PluginsPage, usePluginsFlow } from "../../components/plugins/PluginsFlow";
import { isConnecting } from "../../services/pluginConnectionsModel";

export default function PluginsCatalogScreen() {
  const router = useRouter();
  const flow = usePluginsFlow();
  const connecting = flow.running?.startsWith("connect:") ? flow.running.slice("connect:".length) : isConnecting(flow.phase) ? flow.service : null;
  return <PluginsPage title="Plugins" catalog>
    <CatalogView
      rows={flow.rows}
      loading={flow.running === "list" && !flow.catalog.length}
      error={flow.catalog.length ? "" : flow.error}
      connecting={connecting}
      onRetry={() => void flow.send({ action: "list" })}
      onOpen={(plugin) => router.push(pluginServicePath(plugin.id))}
      onConnect={(plugin) => void flow.connect(plugin.id)}
      onReconnect={(account) => void flow.connect(account.integration)}
    />
  </PluginsPage>;
}
