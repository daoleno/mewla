import React, { useEffect, useState } from "react";
import { useRouter } from "expo-router";
import { CatalogView, ToolUpdatesNotice } from "../../components/plugins/PluginConnectionViews";
import { pluginServicePath, PluginsPage, usePluginsFlow } from "../../components/plugins/PluginsFlow";
import { AgentToolsSection } from "../../components/extensions/AgentToolsSection";
import { useAgentExtensions } from "../../components/extensions/AgentExtensionsProvider";
import { isConnecting } from "../../services/pluginConnectionsModel";

const OFFLINE_SETTLE_MS = 1500;

/**
 * Extensions: Mewla's own plugins first, then the user's own services, then
 * the agents' own plugins and Skills by tool. The route stays /plugins, the
 * address every sign-in returns to (mewla://plugins and the daemon's web
 * redirect).
 */
export default function ExtensionsScreen() {
  const router = useRouter();
  const flow = usePluginsFlow();
  const agent = useAgentExtensions();
  // The socket starts out offline for a moment on every page load; only an
  // offline that lasts means the computer can't be reached.
  const [offlineSettled, setOfflineSettled] = useState(false);
  useEffect(() => {
    setOfflineSettled(false);
    if (flow.connection !== "offline") return;
    const timer = setTimeout(() => setOfflineSettled(true), OFFLINE_SETTLE_MS);
    return () => clearTimeout(timer);
  }, [flow.connection]);
  const connecting = flow.running?.startsWith("connect:") ? flow.running.slice("connect:".length) : isConnecting(flow.phase) ? flow.service : null;
  return <PluginsPage title="Extensions" catalog>
    {flow.accounts.map((account) => <ToolUpdatesNotice
      key={account.id}
      account={account}
      serviceName={flow.serviceLabel(account)}
      running={flow.running}
      onConfirm={(names) => flow.confirmUpdates(account, names)}
    />)}
    <CatalogView
      rows={flow.rows}
      // Until the first list answers, the page is loading, not failed; a
      // server that can't be reached says so once the socket gives up.
      loading={!flow.catalog.length && (flow.running === "list" || (!flow.loaded && !offlineSettled))}
      error={flow.catalog.length ? "" : flow.error || (!flow.loaded && offlineSettled ? "Can't reach your computer." : "")}
      connecting={connecting}
      onRetry={() => void flow.send({ action: "list" })}
      onOpen={(plugin) => router.push(pluginServicePath(plugin.id))}
      onConnect={(plugin) => void flow.connect(plugin.id)}
      onReconnect={(account) => flow.recover(account, "reconnect")}
    />
    <AgentToolsSection agent={agent} onOpen={(scope) => router.push(`/plugins/agents/${scope}`)} />
  </PluginsPage>;
}
