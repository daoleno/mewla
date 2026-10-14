import React, { useState } from "react";
import { useLocalSearchParams, useRouter } from "expo-router";
import { ApiKeyForm } from "../../../components/plugins/ApiKeyForm";
import { CustomServiceForm } from "../../../components/plugins/CustomServiceForm";
import { AccountCard, ConnectOffer, ServiceHeader, ToolUpdatesNotice } from "../../../components/plugins/PluginConnectionViews";
import { pluginServicePath, PluginsPage, usePluginsFlow } from "../../../components/plugins/PluginsFlow";
import { Button, InlineNotice } from "../../../components/ui";
import { isConnecting, usesApiKey } from "../../../services/pluginConnectionsModel";
import { pluginJobs } from "../../../services/pluginOnboarding";
import type { ConnectionRequest } from "../../../services/connections";

export default function PluginServiceScreen() {
  const { service } = useLocalSearchParams<{ service: string }>();
  const router = useRouter();
  const flow = usePluginsFlow();
  const [adding, setAdding] = useState(false);
  const row = [...flow.rows.services, ...flow.rows.custom].find((entry) => entry.plugin.id === service);
  if (!row) {
    return <PluginsPage title="Extensions">
      {flow.loaded && flow.catalog.length ? <InlineNotice title="Service unavailable" detail={`${flow.serverName} does not offer this service.`} /> : null}
    </PluginsPage>;
  }
  const { plugin, accounts } = row;
  const job = pluginJobs[plugin.id];
  const keyed = usesApiKey(plugin.id);
  const custom = plugin.id === "mcp" || plugin.id === "openapi" || keyed;
  const connecting = flow.running === `connect:${plugin.id}` || isConnecting(flow.phase) && flow.service === plugin.id;
  const otherConnecting = !connecting && (flow.running?.startsWith("connect:") || isConnecting(flow.phase));
  const connect = () => void flow.connect(plugin.id);
  const github = plugin.id === "github" ? () => void flow.useServerGitHub() : undefined;
  // A form brings its details; only a custom service's own sign-in opens a browser.
  const submit = (input: NonNullable<ConnectionRequest["input"]>, signIn = false) => flow.connect(plugin.id, { input, signIn });
  const form = keyed ? <ApiKeyForm key={plugin.id} plugin={plugin} job={job} serverName={flow.serverName} connecting={connecting} onConnect={(input) => submit(input)} />
    : <CustomServiceForm key={plugin.id} plugin={plugin} connecting={connecting} serverName={flow.serverName} onConnect={submit} />;
  return <PluginsPage title={plugin.name}>
    <ServiceHeader plugin={plugin} />
    {accounts.map((account) => <ToolUpdatesNotice
      key={`updates:${account.id}`}
      account={account}
      serviceName={flow.serviceLabel(account)}
      running={flow.running}
      onConfirm={(names) => flow.confirmUpdates(account, names)}
    />)}
    {accounts.map((account) => <AccountCard
      key={account.id}
      plugin={plugin}
      account={account}
      job={job}
      running={flow.running}
      onAccess={(group, allowed) => flow.setAccess(account, group, allowed)}
      onRecover={(action) => flow.recover(account, action)}
      onSetEnabled={(on) => flow.setEnabled(account, on)}
      onDisconnect={() => flow.disconnect(account)}
      onSetKey={keyed ? (key) => flow.setKey(account, key) : undefined}
      onOpenTools={() => router.push({ pathname: `${pluginServicePath(plugin.id)}/tools`, params: { account: account.id } } as never)}
    />)}
    {!accounts.length ? custom ? form : <ConnectOffer
      plugin={plugin}
      job={job}
      serverName={flow.serverName}
      connecting={connecting}
      disabled={!!otherConnecting}
      onConnect={connect}
      onUseServerAccount={github}
    /> : custom ? adding ? form : <Button label={keyed ? `Add another ${plugin.name} account` : plugin.id === "mcp" ? "Add another MCP server" : "Add another OpenAPI service"} icon="add" variant="tinted" block onPress={() => setAdding(true)} />
      : plugin.available ? <Button label={`Add another ${plugin.name} account`} icon="add" variant="tinted" block loading={connecting} disabled={!!otherConnecting} onPress={connect} /> : null}
  </PluginsPage>;
}
