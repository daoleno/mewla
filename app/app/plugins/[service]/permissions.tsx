import React from "react";
import { useLocalSearchParams, useRouter } from "expo-router";
import { PermissionsView } from "../../../components/plugins/PluginConnectionViews";
import { pluginServicePath, PluginsPage, usePluginService, usePluginsFlow } from "../../../components/plugins/PluginsFlow";
import { pluginJobs } from "../../../services/pluginOnboarding";

export default function PluginPermissionsScreen() {
  const { service } = useLocalSearchParams<{ service: string }>();
  const router = useRouter();
  const flow = usePluginsFlow();
  usePluginService(service);
  const account = flow.account?.integration === service ? flow.account : null;
  return <PluginsPage title="Permissions">
    {account ? <PermissionsView
      account={account}
      job={pluginJobs[service]}
      busy={flow.busy}
      onChange={flow.changeGroup}
      onReconnect={() => { flow.reconnect(); router.dismissTo(pluginServicePath(service)); }}
    /> : null}
  </PluginsPage>;
}
