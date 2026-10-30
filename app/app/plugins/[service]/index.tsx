import React, { useState } from "react";
import { useLocalSearchParams, useRouter } from "expo-router";
import { AwsForm } from "../../../components/plugins/AwsForm";
import { CustomServiceForm } from "../../../components/plugins/CustomServiceForm";
import { HyperliquidForm } from "../../../components/plugins/HyperliquidForm";
import { AccountCard, ConnectOffer, ServiceHeader, ToolUpdatesNotice } from "../../../components/plugins/PluginConnectionViews";
import { pluginServicePath, PluginsPage, usePluginsFlow } from "../../../components/plugins/PluginsFlow";
import { Button, InlineNotice } from "../../../components/ui";
import { isConnecting } from "../../../services/pluginConnectionsModel";
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
  const custom = plugin.id === "mcp" || plugin.id === "openapi";
  const connecting = flow.running === `connect:${plugin.id}` || isConnecting(flow.phase) && flow.service === plugin.id;
  const otherConnecting = !connecting && (flow.running?.startsWith("connect:") || isConnecting(flow.phase));
  const connect = () => void flow.connect(plugin.id);
  const github = plugin.id === "github" ? () => void flow.useServerGitHub() : undefined;
  // A form brings its details; only a custom service's own sign-in opens a browser.
  const submit = (input: NonNullable<ConnectionRequest["input"]>, signIn = false) => flow.connect(plugin.id, { input, signIn });
  // Hyperliquid's reading is its account above; the form below is the optional API wallet key for trading.
  const trading = plugin.id === "hyperliquid";
  const aws = plugin.id === "aws";
  const form = aws
    ? <AwsForm key={plugin.id} plugin={plugin} accounts={accounts} profiles={flow.awsProfiles} connecting={connecting} serverName={flow.serverName} onLoad={flow.loadAwsProfiles} onConnect={submit} />
    : trading
    ? <HyperliquidForm key={plugin.id} plugin={plugin} connecting={connecting} serverName={flow.serverName} defaultOpen={adding} onConnect={submit} />
    : <CustomServiceForm key={plugin.id} plugin={plugin} connecting={connecting} serverName={flow.serverName} onConnect={submit} />;
  const addAnother = (label: string) => <Button label={label} icon="add" variant="tinted" block onPress={() => setAdding(true)} />;
  const tail = aws
    // Pasted keys that stopped working are replaced from the same form.
    ? !accounts.length || adding || accounts.some((account) => account.status === "authorization_required" && !account.profile) ? form : addAnother("Add another AWS account")
    : trading
    ? accounts.some((account) => account.signer) ? adding ? form : addAnother("Add another key") : form
    : custom
      ? !accounts.length || adding ? form : addAnother(plugin.id === "mcp" ? "Add another MCP server" : "Add another OpenAPI service")
      : !accounts.length
        ? <ConnectOffer plugin={plugin} job={job} serverName={flow.serverName} connecting={connecting} disabled={!!otherConnecting} onConnect={connect} onUseServerAccount={github} />
        : plugin.available ? <Button label={`Add another ${plugin.name} account`} icon="add" variant="tinted" block loading={connecting} disabled={!!otherConnecting} onPress={connect} /> : null;
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
      onRemoveKey={trading ? () => flow.removeKey(account) : undefined}
      onOpenTools={() => router.push({ pathname: `${pluginServicePath(plugin.id)}/tools`, params: { account: account.id } } as never)}
    />)}
    {tail}
  </PluginsPage>;
}
