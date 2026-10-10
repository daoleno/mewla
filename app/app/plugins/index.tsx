import React from "react";
import { useRouter } from "expo-router";
import { CatalogView, MewlaPluginsHeading } from "../../components/plugins/PluginConnectionViews";
import { pluginServicePath, PluginsPage, usePluginsFlow } from "../../components/plugins/PluginsFlow";
import { AgentToolsSection } from "../../components/extensions/AgentToolsSection";
import { useAgentExtensions } from "../../components/extensions/AgentExtensionsProvider";
import { isConnecting } from "../../services/pluginConnectionsModel";

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
  const connecting = flow.running?.startsWith("connect:") ? flow.running.slice("connect:".length) : isConnecting(flow.phase) ? flow.service : null;
  return <PluginsPage title="Extensions" catalog>
    <MewlaPluginsHeading />
    <CatalogView
      rows={flow.rows}
      loading={flow.running === "list" && !flow.catalog.length}
      error={flow.catalog.length ? "" : flow.error}
      connecting={connecting}
      onRetry={() => void flow.send({ action: "list" })}
      onOpen={(plugin) => router.push(pluginServicePath(plugin.id))}
      onConnect={(plugin) => void flow.connect(plugin.id)}
      onReconnect={(account) => flow.recover(account, "reconnect")}
    />
    <AgentToolsSection agent={agent} onOpen={(scope) => router.push(`/plugins/agents/${scope}`)} />
  </PluginsPage>;
}
