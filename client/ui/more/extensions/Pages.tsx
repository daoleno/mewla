import { computed, effect, signal, Text, untracked, View } from "nsh";
import type { JSX, ScrollRef } from "nsh";
import { cloudProfilesSummary } from "../../../core/extensions/cloudProfilesModel";
import { isConnecting, pluginCatalogRows, toolUpdates, type ConnectionTone, type ServiceRow } from "../../../core/extensions/pluginConnectionsModel";
import { pluginJobs } from "../../../core/extensions/pluginOnboarding";
import { connectionStateTone } from "../../../core/extensions/colour";
import { daemon } from "../daemon";
import { color } from "../../kit/colors";
import { EmptyState, Notice, Page, Row, RowButton, SearchField, Section, Spinner, Tag, type Status } from "../../kit/kit";
import { stateTextColor } from "../../kit/RowTile";
import type { PageNav } from "../routes";
import { AgentPage } from "./agents/AgentPage";
import { AgentToolsSection } from "./agents/AgentToolsSection";
import { ConnectCard } from "./ConnectCard";
import { catalogLoading, catalogProblem, deferredFor, flowState, plugins } from "./plugins";
import type { ExtensionsPage } from "./routes";
import { serviceRowLead } from "./ServiceMark";
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

/** The service whose sign-in is starting or under way: its row spins, the others wait. */
export function connectingService(): string | null {
  const state = flowState();
  if (state.running?.startsWith("connect:")) return state.running.slice("connect:".length);
  return isConnecting(state.phase) ? state.service : null;
}

/**
 * What every Plugins page has above its own content: a return kept for another
 * computer, a request that failed, the sign-in in progress. Without a current
 * computer, only the way to choose one.
 */
export function PluginsFrame(props: { title: string; nav: PageNav; testID?: string; onRefresh?: () => void; hero?: JSX.Element; children?: JSX.Element }) {
  let scroll: ScrollRef | null = null;
  // A card that asks for an address, a code or a CLI install comes into view, even
  // when it was started from the bottom of a long page.
  const asking = computed(
    () => {
      const state = flowState();
      const f = state.flow?.flow;
      return f && (f.address_required || f.code_required || f.install_required) && (state.phase === "waiting" || state.phase === "verifying") ? f.id : null;
    },
    { equals: (a, b) => a === b },
  );
  effect(() => {
    if (asking()) untracked(() => setTimeout(() => scroll?.scrollTo({ y: 0, animated: false }), 0));
  });
  return (
    <Page display hero={props.hero} title={props.title} testID={props.testID} scrollRef={(ref) => (scroll = ref)} refreshing={flowState().running === "list"} onRefresh={props.onRefresh ?? (() => void plugins()?.list())}>
      {deferredFor() ? <Notice testID="authorization-saved" icon="swap-horizontal" title={`Authorization saved for ${deferredFor()}`} detail="Switch to that server in Settings to finish." action="Settings" onAction={() => props.nav.push({ name: "settings" })} /> : null}
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
          return updates ? (
            <Notice
              testID={`tool-updates-${account.id}`}
              tone="info"
              icon="sync"
              title={updates.title}
              detail={updates.detail}
              busy={flowState().running === `updates:${account.id}`}
              actionDisabled={flowState().running !== null}
              action="Confirm"
              onAction={() => flowState().running === null && plugins()?.confirmUpdates(account, updates.names)}
            />
          ) : null;
        })}
    </View>
  );
}

/**
 * Extensions: the services Brain may use on this computer, by state (SPEC §15 Collection): Connected,
 * Needs fixing, Available; then what each agent installed itself. The search narrows every section.
 */
function Extensions(props: { nav: PageNav }) {
  const query = signal("");
  const rows = () => pluginCatalogRows(flowState().catalog, flowState().accounts, pluginJobs);
  const groups = () => {
    const q = query().trim().toLowerCase();
    const all = [...rows().services.map((row) => ({ row, custom: false })), ...rows().custom.map((row) => ({ row, custom: true }))]
      .filter(({ row }) => !q || `${row.plugin.name} ${row.plugin.description ?? ""} ${accountNames(row)}`.toLowerCase().includes(q));
    return {
      connected: all.filter(({ row }) => row.state.kind === "status"),
      fixing: all.filter(({ row }) => row.state.kind === "reconnect"),
      available: all.filter(({ row }) => row.state.kind === "connect" || row.state.kind === "unavailable"),
    };
  };
  const section = (title: string, items: { row: ServiceRow; custom: boolean }[]) =>
    items.length ? <Section title={title}>{items.map(({ row, custom }) => <ServiceRowView row={row} nav={props.nav} custom={custom} />)}</Section> : null;
  return (
    <PluginsFrame title="Extensions" testID="extensions-page" nav={props.nav}>
      <ToolUpdateNotices />
      {catalogLoading() ? (
        <EmptyState busy title="Loading services" />
      ) : !flowState().catalog.length ? (
        catalogProblem() ? (
          <EmptyState testID="services-unavailable" icon="cloud-offline" title="Services unavailable" detail={catalogProblem()} action="Try again" onAction={() => void plugins()?.list()} />
        ) : (
          <EmptyState icon="puzzle" title="No services" action="Try again" onAction={() => void plugins()?.list()} />
        )
      ) : (
        <View>
          <View class="mt-2">
            <SearchField testID="extensions-search" placeholder="Search services" value={query()} onChange={(value) => query.set(value)} />
          </View>
          {section("Connected", groups().connected)}
          {section("Needs fixing", groups().fixing)}
          <AgentToolsSection nav={props.nav} />
          {section("Available", groups().available)}
          {query().trim() && !groups().connected.length && !groups().fixing.length && !groups().available.length ? <EmptyState title="No matching services" /> : null}
        </View>
      )}
      {catalogLoading() || !flowState().catalog.length ? <AgentToolsSection nav={props.nav} /> : null}
    </PluginsFrame>
  );
}

/** Who is connected: the cloud summary for AWS and Google Cloud, a count past two accounts, else their names. */
function accountNames(row: ServiceRow): string {
  const { plugin, accounts } = row;
  if (plugin.id === "aws" || plugin.id === "gcp") return cloudProfilesSummary(accounts);
  return accounts.length > 2 ? `${accounts.length} accounts` : accounts.map((account) => account.name).join(" · ");
}

function rowSubtitle(row: ServiceRow): string {
  const plugin = row.plugin;
  if (row.state.kind === "unavailable") return plugin.unavailable_reason || "Sign-in is not set up yet.";
  // What needs fixing says so, in the warn colour (SPEC §10.4), with whose sign-in it is.
  if (row.state.kind === "reconnect") return [`Sign-in expired`, accountNames(row)].filter(Boolean).join(" · ");
  if (plugin.id === "gcp" && !row.accounts.length) return "Nothing connected yet";
  return accountNames(row);
}

/** What a screen reader says for the row: the service and its state, which the row's parts say apart. */
function rowLabel(row: ServiceRow): string {
  const state = row.state;
  if (state.kind === "reconnect") return `${row.plugin.name}, ${state.account.name} needs sign-in again`;
  if (state.kind === "status") return `${row.plugin.name}, ${accountNames(row)}, ${state.label}`;
  if (state.kind === "unavailable") return `${row.plugin.name}, ${state.label}. ${rowSubtitle(row)}`;
  return row.plugin.name;
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
  const connecting = () => connectingService() === id;
  // While one sign-in starts or waits, the other services wait for it.
  const blocked = () => flowState().running !== null || (connectingService() !== null && !connecting());
  const trailing = (): JSX.Element => {
    const state = row.state;
    if (state.kind === "unavailable") return <Tag label={state.label} />;
    if (state.kind === "connect" || state.kind === "reconnect") {
      const label = state.kind === "reconnect" ? "Reconnect" : props.custom ? "Add" : "Connect";
      return connecting() ? (
        <Spinner size={16} />
      ) : (
        <RowButton
          testID={`connect-${id}`}
          label={label}
          accessibilityLabel={state.kind === "reconnect" ? `Reconnect ${state.account.name}` : `${label} ${row.plugin.name}`}
          disabled={blocked()}
          onPress={() => (state.kind === "reconnect" ? plugins()?.recover(state.account, "reconnect") : connect())}
        />
      );
    }
    // Grouped by state, the section label says it: no dot, only a word that needs a look is coloured (SPEC §10.4).
    return connecting() ? <Spinner size={16} /> : <Text testID={`service-${id}-state`} class="text-sub line-clamp-1" style={{ color: color(stateTextColor(connectionStateTone(state.tone) === "warn" ? "warn" : "off")) }}>{state.label}</Text>;
  };
  const available = row.state.kind !== "unavailable";
  return (
    <Row
      testID={`service-${id}`}
      lead={serviceRowLead(id)}
      title={row.plugin.name}
      subtitle={rowSubtitle(row) || undefined}
      subtitleTone={row.state.kind === "reconnect" ? "warn" : undefined}
      trailing={trailing()}
      chevron={available}
      disabled={!available}
      accessibilityLabel={rowLabel(row)}
      accessibilityHint={available ? "Opens the service" : undefined}
      onPress={available ? open : undefined}
    />
  );
}
