import { Text, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { cloudProfilesSummary } from "../../../core/extensions/cloudProfilesModel";
import { pluginCatalogRows, toolUpdates, type ConnectionTone, type ServiceRow } from "../../../core/extensions/pluginConnectionsModel";
import { endpointHost } from "../../../core/extensions/accountListModel";
import { pluginJobs } from "../../../core/extensions/pluginOnboarding";
import { daemon, daemonState } from "../daemon";
import { EmptyState, Notice, Page, Row, Section, Spinner, StatusMark, Tag, type Status } from "../kit/kit";
import type { PageNav } from "../routes";
import { AgentPage } from "./agents/AgentPage";
import { AgentToolsSection } from "./agents/AgentToolsSection";
import { ConnectCard } from "./ConnectCard";
import { catalogLoading, deferredFor, flowState, plugins } from "./plugins";
import type { ExtensionsPage } from "./routes";
import { ServiceMark } from "./ServiceMark";
import { ServicePage } from "./Service";
import { ToolsPage } from "./Tools";

export function ExtensionsPageView(props: { route: ExtensionsPage; nav: PageNav }) {
  const route = props.route;
  switch (route.name) {
    case "extensions": return <Extensions nav={props.nav} />;
    case "extensions-service": return <ServicePage id={route.id} nav={props.nav} />;
    case "extensions-tools": return <ToolsPage service={route.service} account={route.account} nav={props.nav} />;
    case "extensions-agent": return <AgentPage scope={route.scope} nav={props.nav} />;
  }
}

export function toneStatus(tone: ConnectionTone): Status {
  return tone === "success" ? "ready" : tone === "danger" ? "failed" : tone === "accent" ? "needs" : tone;
}

/**
 * What every Plugins page has above its own content: a return kept for another
 * computer, a request that failed, the sign-in in progress. Without a current
 * computer, only the way to choose one.
 */
export function PluginsFrame(props: { title: string; nav: PageNav; testID?: string; onRefresh?: () => void; children?: JSX.Element }) {
  return (
    <Page title={props.title} testID={props.testID} refreshing={flowState().running === "list"} onRefresh={props.onRefresh ?? (() => void plugins()?.list())}>
      {deferredFor() ? <Notice title={`Authorization saved for ${deferredFor()}`} detail="Switch to that server in Settings to finish." action="Settings" onAction={() => props.nav.push({ name: "settings" })} /> : null}
      {flowState().error && flowState().catalog.length ? <Notice tone="failed" testID="plugins-error" title="That didn't go through" detail={flowState().error} /> : null}
      {daemon() ? <ConnectCard nav={props.nav} /> : null}
      {!daemon() ? <EmptyState icon="server" title="No current server" detail="Choose one in Settings." action="Open Settings" onAction={() => props.nav.push({ name: "settings" })} /> : props.children}
    </Page>
  );
}

/** Notices for tools a service added or changed, which wait for a confirm. */
export function ToolUpdateNotices(props: { service?: string }) {
  return (
    <View>
      {flowState()
        .accounts.filter((account) => !props.service || account.integration === props.service)
        .map((account) => {
          const updates = toolUpdates(account, plugins()?.serviceLabel(account) ?? account.name);
          return updates ? <Notice title={updates.title} detail={updates.detail} action="Confirm" onAction={() => flowState().running === null && plugins()?.confirmUpdates(account, updates.names)} /> : null;
        })}
    </View>
  );
}

/** Extensions: the services Brain may use on this computer, and what each agent installed itself. */
function Extensions(props: { nav: PageNav }) {
  const rows = () => pluginCatalogRows(flowState().catalog, flowState().accounts, pluginJobs);
  return (
    <PluginsFrame title="Extensions" testID="extensions-page" nav={props.nav}>
      <ToolUpdateNotices />
      {catalogLoading() ? (
        <EmptyState busy title="Loading services" />
      ) : !flowState().catalog.length ? (
        flowState().error || daemonState() !== "connected" ? (
          <EmptyState testID="services-unavailable" icon="cloud-offline" title="Services unavailable" detail={flowState().error || "Can't reach your computer."} action="Try again" onAction={() => void plugins()?.list()} />
        ) : (
          <EmptyState title="No services" detail="This server has none to connect." action="Try again" onAction={() => void plugins()?.list()} />
        )
      ) : (
        <View>
          <Section title="Mewla plugins">{rows().services.map((row) => <ServiceRowView row={row} nav={props.nav} />)}</Section>
          {rows().custom.length ? <Section title="Your own services" footer="Any MCP server or OpenAPI service, by its address.">{rows().custom.map((row) => <ServiceRowView row={row} nav={props.nav} custom />)}</Section> : null}
        </View>
      )}
      <AgentToolsSection nav={props.nav} />
    </PluginsFrame>
  );
}

function rowSubtitle(row: ServiceRow, custom: boolean): string {
  const plugin = row.plugin;
  if (!plugin.available) return plugin.unavailable_reason || "Sign-in is not set up yet.";
  if (custom) return row.accounts.map((account) => endpointHost(account.endpoint) || account.name).join(" · ") || plugin.description;
  if (plugin.id === "gcp" && !row.accounts.length) return "Nothing connected yet";
  if (["aws", "gcp", "gws"].includes(plugin.id) && row.accounts.length) return cloudProfilesSummary(row.accounts);
  return row.accounts.map((account) => account.name).join(" · ") || plugin.description;
}

function ServiceRowView(props: { row: ServiceRow; nav: PageNav; custom?: boolean }) {
  const row = props.row;
  const id = row.plugin.id;
  const open = () => props.nav.push({ name: "extensions-service", id });
  const connect = () => {
    void plugins()
      ?.connect(id)
      .then((outcome) => outcome === "form" && open());
  };
  const busy = () => flowState().running !== null;
  const connecting = () => flowState().running === `connect:${id}`;
  const trailing = (): JSX.Element => {
    const state = row.state;
    if (state.kind === "unavailable") return <Tag label={state.label} />;
    if (state.kind === "connect" || state.kind === "reconnect") {
      const label = state.kind === "reconnect" ? "Reconnect" : props.custom ? "Add" : "Connect";
      return connecting() ? (
        <Spinner size={16} />
      ) : (
        <Button
          testID={`connect-${id}`}
          accessibilityLabel={state.kind === "reconnect" ? `Reconnect ${state.account.name}` : `Connect ${row.plugin.name}`}
          disabled={busy()}
          onPress={() => (state.kind === "reconnect" ? plugins()?.recover(state.account, "reconnect") : connect())}
          class="rounded-full bg-secondary px-3 py-1.5 active:opacity-70 disabled:opacity-50"
        >
          <Text class="text-sm font-medium">{label}</Text>
        </Button>
      );
    }
    return <StatusMark status={toneStatus(state.tone)} label={state.label} />;
  };
  return (
    <Row
      testID={`service-${id}`}
      leading={<ServiceMark id={id} />}
      title={row.plugin.name}
      subtitle={rowSubtitle(row, !!props.custom)}
      trailing={trailing()}
      chevron={row.state.kind === "status"}
      disabled={!row.plugin.available}
      onPress={row.plugin.available ? open : undefined}
    />
  );
}
