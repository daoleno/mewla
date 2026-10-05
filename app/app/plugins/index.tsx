import React from "react";
import { useRouter } from "expo-router";
import { CatalogView } from "../../components/plugins/PluginConnectionViews";
import { pluginServicePath, PluginsPage, usePluginsFlow } from "../../components/plugins/PluginsFlow";

export default function PluginsCatalogScreen() {
  const router = useRouter();
  const flow = usePluginsFlow();
  return <PluginsPage title="Plugins" catalog>
    <CatalogView
      sections={flow.sections}
      loading={flow.busy}
      error={flow.catalog.length ? "" : flow.error}
      onRetry={() => void flow.send({ action: "list" })}
      onOpenAccount={(plugin, item) => router.push({ pathname: "/plugins/[service]", params: { service: plugin.id, account: item.id } })}
      onOpenService={(plugin) => router.push(pluginServicePath(plugin.id))}
      onOpenCustom={() => router.push("/plugins/custom")}
    />
  </PluginsPage>;
}
