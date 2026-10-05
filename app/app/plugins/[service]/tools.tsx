import React from "react";
import { useLocalSearchParams } from "expo-router";
import { ToolsView } from "../../../components/plugins/PluginConnectionViews";
import { PluginsPage, usePluginService, usePluginsFlow } from "../../../components/plugins/PluginsFlow";

export default function PluginToolsScreen() {
  const { service } = useLocalSearchParams<{ service: string }>();
  const flow = usePluginsFlow();
  usePluginService(service);
  const account = flow.account?.integration === service ? flow.account : null;
  return <PluginsPage title="Tools & activity">
    {account ? <ToolsView
      account={account}
      busy={flow.busy}
      onToggleEnabled={(enabled) => void flow.send({ action: enabled ? "enable" : "disable", id: account.id })}
      onRefresh={() => void flow.send({ action: "refresh", id: account.id })}
      onToggleTool={(tool, allowed) => flow.toggleTool(account, tool, allowed)}
    /> : null}
  </PluginsPage>;
}
