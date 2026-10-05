import React from "react";
import { useRouter } from "expo-router";
import { CustomCatalogView } from "../../components/plugins/PluginConnectionViews";
import { pluginServicePath, PluginsPage, usePluginsFlow } from "../../components/plugins/PluginsFlow";

export default function PluginsCustomScreen() {
  const router = useRouter();
  const flow = usePluginsFlow();
  return <PluginsPage title="Custom services">
    <CustomCatalogView sections={flow.sections} onOpenService={(plugin) => router.push(pluginServicePath(plugin.id))} />
  </PluginsPage>;
}
