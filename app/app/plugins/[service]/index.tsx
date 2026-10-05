import React from "react";
import { useLocalSearchParams, useRouter } from "expo-router";
import * as Clipboard from "expo-clipboard";
import { CustomServiceForm } from "../../../components/plugins/CustomServiceForm";
import { ConnectOfferView, ConnectProgressCard, LinkedAccountView, ServiceHeader } from "../../../components/plugins/PluginConnectionViews";
import { pluginServicePath, PluginsPage, usePluginService, usePluginsFlow } from "../../../components/plugins/PluginsFlow";
import { InlineNotice } from "../../../components/ui";
import { isConnecting } from "../../../services/pluginConnectionsModel";
import { pluginJobs } from "../../../services/pluginOnboarding";

export default function PluginServiceScreen() {
  const { service, account: accountId } = useLocalSearchParams<{ service: string; account?: string }>();
  const router = useRouter();
  const flow = usePluginsFlow();
  usePluginService(service, { accountId, owner: true });
  const selected = flow.selected?.id === service ? flow.selected : null;
  if (!selected) {
    return <PluginsPage title="Plugins">
      {flow.loaded && flow.catalog.length ? <InlineNotice title="Service unavailable" detail={`${flow.serverName} does not offer this service.`} /> : null}
    </PluginsPage>;
  }
  const { account, phase } = flow;
  const job = pluginJobs[selected.id];
  const custom = selected.id === "mcp" || selected.id === "openapi";
  const linked = account && !flow.another && account.status !== "disconnected" ? account : null;
  const accountCount = flow.accounts.filter((a) => a.status !== "disconnected" && a.integration === selected.id).length;
  const path = pluginServicePath(selected.id);
  return <PluginsPage title={selected.name}>
    <ServiceHeader plugin={selected} account={linked} />
    {isConnecting(phase) ? <ConnectProgressCard serviceName={selected.name} phase={phase} flow={flow.flow} onCancel={() => void flow.cancel()} onOpen={() => void (async () => {
      if (!flow.flow) return;
      if (flow.flow.flow.user_code) await Clipboard.setStringAsync(flow.flow.flow.user_code);
      await flow.openBrowser(flow.flow);
    })()} /> : linked ? <LinkedAccountView
      plugin={selected}
      account={linked}
      job={job}
      custom={custom}
      busy={flow.busy}
      accountCount={accountCount}
      onRecover={flow.recover}
      onOpenPermissions={() => router.push(`${path}/${custom ? "tools" : "permissions"}`)}
      onOpenAccounts={() => router.push(`${path}/accounts`)}
      onOpenTools={() => router.push(`${path}/tools`)}
    /> : custom ? <CustomServiceForm key={selected.id} plugin={selected} busy={flow.busy} serverName={flow.serverName} send={flow.send} authorize={flow.authorize} /> : <ConnectOfferView
      plugin={selected}
      job={job}
      writes={flow.writes}
      busy={flow.busy}
      serverName={flow.serverName}
      retry={phase === "failed" || phase === "cancelled"}
      confirmIdentity={flow.flow?.flow.status === "confirm" ? flow.flow.flow.identity ?? "" : null}
      onWritesChange={flow.setWrites}
      onConnect={() => void flow.authorize({ integration: selected.id, allow_writes: flow.writes })}
      onUseServerAccount={selected.id === "github" ? () => void flow.previewGitHub() : undefined}
      onImport={() => void flow.importGitHub()}
      onChooseAnother={() => void flow.cancel()}
      onRetryVerification={phase === "failed" && flow.flow?.callback ? () => void flow.check() : undefined}
    />}
  </PluginsPage>;
}
