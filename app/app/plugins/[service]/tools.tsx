import React, { useEffect } from "react";
import { useLocalSearchParams } from "expo-router";
import { ToolsView } from "../../../components/plugins/PluginConnectionViews";
import { PluginsPage, usePluginsFlow } from "../../../components/plugins/PluginsFlow";
import { EmptyState } from "../../../components/ui";

export default function PluginToolsScreen() {
  const { service, account: accountId } = useLocalSearchParams<{ service: string; account?: string }>();
  const flow = usePluginsFlow();
  // Without an account in the link, the service's first account.
  const id = accountId ?? flow.accounts.find((item) => item.integration === service && item.status !== "disconnected")?.id;
  const { loaded, load } = flow;
  useEffect(() => { if (loaded && id) void load(id); }, [id, load, loaded]);
  const account = id ? flow.details[id] : undefined;
  return <PluginsPage title="Tools & activity">
    {account ? <ToolsView
      account={account}
      running={flow.running}
      onToggleEnabled={(enabled) => void flow.send({ action: enabled ? "enable" : "disable", id: account.id }, `recover:${account.id}`)}
      onRefresh={() => void flow.send({ action: "refresh", id: account.id }, `recover:${account.id}`)}
      onToggleTool={(tool, allowed) => flow.toggleTool(account, tool, allowed)}
    /> : flow.running === `load:${id}` || !flow.loaded ? <EmptyState busy title="Loading tools" /> : null}
  </PluginsPage>;
}
