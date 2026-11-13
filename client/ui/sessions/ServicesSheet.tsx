import { cleanup, Clipboard, effect, openUrl, Scroll, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { EmptyState, Notice, Row, Section, SectionLabel, Spinner } from "../kit/kit";
import { Sheet } from "../kit/Sheet";
import { Icon } from "../kit/Icon";
import { listSessionServices, serviceTunnel, type ServiceTunnel } from "../../core/sessions/services";
import {
  groupSessionServices,
  hasServiceTerminal,
  isDSHWebService,
  presentSessionServiceURL,
  serviceBindLabel,
  serviceCommandDetail,
  serviceProcessLabel,
  serviceProjectLabel,
  serviceSourceLabel,
  serviceWorkerLabel,
  type DiscoveredSessionService,
} from "../../core/sessions/servicesPresentation";
import { connection, current, currentTransport } from "../session";
import { showAlert } from "../shell/alert";

/** Open from the Sessions list's menu. */
export const servicesOpen = signal(false);

const keyOf = (service: DiscoveredSessionService) => `${service.serverId}:${service.id}`;
const messageOf = (error: unknown, fallback: string) => (error instanceof Error && error.message ? error.message : fallback);
const capitalize = (value?: string) => (value ? value.charAt(0).toUpperCase() + value.slice(1) : "");

function open(url: string) {
  void openUrl(url).catch(() => {});
}

/**
 * One port's page: open its links or its Session's terminal, share it through
 * a Quick Tunnel (asks first; the status is read every 2 s while it starts,
 * runs or stops), and what it is.
 */
function ServiceDetail(props: { service: () => DiscoveredSessionService; onBack: () => void; onOpenSession: (workerId: string) => void }) {
  const transport = untracked(currentTransport);
  const tunnel = signal<ServiceTunnel | undefined>(untracked(props.service).tunnel);
  const busy = signal(false);
  const problem = signal("");
  const copied = signal(false);
  let alive = true;
  let copiedTimer: ReturnType<typeof setTimeout> | undefined;
  cleanup(() => {
    alive = false;
    clearTimeout(copiedTimer);
  });
  const active = () => ["starting", "running", "stopping"].includes(tunnel()?.status ?? "");
  const publicURL = () => (tunnel()?.status === "running" ? tunnel()?.url : undefined);
  const persistent = () => !hasServiceTerminal(props.service()) && props.service().source === "persistent";
  const subtitle = () => (persistent() ? serviceSourceLabel(props.service()) : serviceWorkerLabel(props.service()));

  const run = async (action: "start" | "stop" | "status") => {
    const service = untracked(props.service);
    if (!transport || !service.generation) return;
    if (action !== "status") {
      busy.set(true);
      problem.set("");
    }
    try {
      const next = await serviceTunnel(transport, service.id, service.generation, action);
      if (alive) tunnel.set(next);
    } catch (error) {
      if (alive) problem.set(messageOf(error, "Tunnel unavailable"));
    } finally {
      if (alive && action !== "status") busy.set(false);
    }
  };
  effect(() => {
    if (!active()) return;
    const timer = setInterval(() => void run("status"), 2000);
    cleanup(() => clearInterval(timer));
  });
  const start = async () => {
    const { action } = await showAlert({
      title: "Share this service publicly?",
      description: "Anyone with the temporary URL can access this service. Quick Tunnels do not support SSE and allow 200 in-flight requests.",
      actions: [{ id: "cancel", label: "Cancel", role: "cancel" }, { id: "start", label: "Start tunnel" }],
    });
    if (action === "start") void run("start");
  };
  const copy = async (url: string) => {
    await Clipboard.writeText(url).catch(() => {});
    copied.set(true);
    clearTimeout(copiedTimer);
    copiedTimer = setTimeout(() => copied.set(false), 1600);
  };

  const urls = () => (props.service().urls ?? []).map(presentSessionServiceURL);
  return (
    <View class="px-4">
      <View class="px-4"><Text class="text-sub text-muted-foreground line-clamp-1">{`Port ${props.service().port} · ${subtitle()}`}</Text></View>
      <Scroll keyboardShouldPersistTaps="handled" style={{ maxHeight: 520 }}>
        <View class="pb-2">
          <Section sheet title="Open">
            {isDSHWebService(props.service()) ? (
              urls().length > 0
                ? <Row testID="service-open-web" icon="browser" title="Open Web" subtitle={urls()[0]!.address} chevron onPress={() => open(urls()[0]!.url)} />
                : <Row icon="browser" title="Web unavailable" disabled />
            ) : urls().length > 0 ? (
              urls().map((item) => <Row testID="service-url" icon="open-external" title={item.label} subtitle={item.address} chevron onPress={() => open(item.url)} />)
            ) : (
              <Row icon="link" title="Bound locally" value={serviceBindLabel(props.service())} />
            )}
            {hasServiceTerminal(props.service()) ? (
              <Row testID="service-terminal" icon="terminal" title="Open Session terminal" chevron onPress={() => props.onOpenSession(props.service().worker_id)} />
            ) : null}
          </Section>
          {props.service().generation ? (
            <Section sheet title="Public access">
              {publicURL() ? <Row testID="tunnel-open" icon="browser" title="Open public URL" subtitle={publicURL()} chevron onPress={() => open(publicURL()!)} /> : null}
              {publicURL() ? <Row testID="tunnel-copy" icon={copied() ? "check" : "copy"} title={copied() ? "Copied" : "Copy public URL"} onPress={() => void copy(publicURL()!)} /> : null}
              <Row testID="tunnel-toggle" icon={active() ? "stop-circle" : "share"} destructive={active()} disabled={busy()}
                trailing={busy() ? <Spinner size={16} /> : undefined}
                title={active() ? "Stop public tunnel" : "Share with Quick Tunnel"}
                subtitle={active() && !publicURL() ? capitalize(tunnel()?.status) : undefined}
                onPress={() => (untracked(active) ? void run("stop") : void start())} />
            </Section>
          ) : null}
          {problem() || tunnel()?.error ? <Notice testID="tunnel-problem" tone="failed" title="Tunnel unavailable" detail={problem() || tunnel()?.error || ""} /> : null}
          <Section sheet title="Details">
            <Row title="Process" value={serviceProcessLabel(props.service())} />
            {serviceCommandDetail(props.service()) ? <Row title="Command" subtitle={serviceCommandDetail(props.service()) ?? undefined} /> : null}
            <Row title="Bind" value={serviceBindLabel(props.service())} />
            {persistent() && props.service().status_detail?.trim() ? <Row title="Status" subtitle={props.service().status_detail!.trim()} /> : null}
          </Section>
        </View>
      </Scroll>
    </View>
  );
}

/**
 * Services: the computer's listening ports, one row per port by project; a
 * port's page holds everything that can be done with it. Read on opening and
 * on Refresh; leaving a port's page reads again (its tunnel may have changed).
 */
export function ServicesSheet(props: { onOpenSession: (workerId: string) => void }) {
  const services = signal<DiscoveredSessionService[]>([]);
  const loading = signal(false);
  const problem = signal("");
  const selected = signal("");
  let epoch = 0;

  const refresh = async () => {
    const transport = untracked(currentTransport);
    const server = untracked(current);
    const mine = ++epoch;
    if (!transport || !server || untracked(connection) !== "connected") {
      services.set([]);
      problem.set("");
      return;
    }
    loading.set(true);
    problem.set("");
    try {
      const snapshot = await listSessionServices(transport);
      if (mine !== epoch) return;
      services.set(snapshot.services
        .map((service) => ({ ...service, serverId: server.id, serverName: server.name }))
        .sort((a, b) => serviceProjectLabel(a).localeCompare(serviceProjectLabel(b)) || a.port - b.port));
    } catch (error) {
      if (mine !== epoch) return;
      services.set([]);
      problem.set(messageOf(error, "Failed to load session services."));
    } finally {
      if (mine === epoch) loading.set(false);
    }
  };
  effect(() => {
    if (servicesOpen()) untracked(() => void refresh());
    else {
      selected.set("");
      epoch++;
    }
  });

  const count = () => services().length;
  const subtitle = () => (loading() && count() === 0 ? "Looking for listening ports" : count() === 0 ? "" : `${count()} ${count() === 1 ? "port" : "ports"}`);
  const selectedService = () => services().find((service) => keyOf(service) === selected());

  const back = () => {
    selected.set("");
    void refresh();
  };
  return (
    <Sheet
      open={servicesOpen}
      onClose={() => servicesOpen.set(false)}
      testID="services-sheet"
      title={() => (selected() && selectedService() ? serviceProcessLabel(selectedService()!) : "Services")}
      leading={selected() && selectedService() ? (
        <Button testID="service-back" accessibilityLabel="Back to services" onPress={back} class="-ml-2 size-11 items-center justify-center rounded-full active:bg-accent">
          <Icon name="chevron-left" size={22} tone="ink" />
        </Button>
      ) : undefined}
      trailing={selected() && selectedService() ? undefined : (
        <View class="flex-row items-center gap-1">
          {subtitle() ? <Text class="mr-1 text-sub text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{subtitle()}</Text> : null}
          <Button testID="services-refresh" accessibilityLabel="Refresh services" disabled={loading()} onPress={() => void refresh()} class="size-11 items-center justify-center rounded-full active:bg-accent disabled:opacity-40">
            <Icon name="refresh" size={20} tone="ink" />
          </Button>
          <Button testID="services-close" accessibilityLabel="Close services" onPress={() => servicesOpen.set(false)} class="size-11 items-center justify-center rounded-full active:bg-accent">
            <Icon name="close" size={20} tone="ink" />
          </Button>
        </View>
      )}
    >
      {selected() && selectedService() ? (
        <ServiceDetail service={() => selectedService()!} onOpenSession={(workerId) => {
          servicesOpen.set(false);
          props.onOpenSession(workerId);
        }} onBack={back} />
      ) : (
        <View class="px-4">
          {problem() ? <Notice testID="services-problem" tone="failed" title="Services could not be loaded" detail={problem()} action="Retry" actionDisabled={loading()} onAction={() => void refresh()} /> : null}
          {count() === 0 && !problem() ? <EmptyState testID="services-empty" icon="radio" busy={loading()} title={loading() ? "Loading services" : "No listening services"} /> : null}
          {count() > 0 ? (
            <Scroll keyboardShouldPersistTaps="handled" style={{ maxHeight: 520 }}>
              <View class="pb-2">
                {groupSessionServices(services()).flatMap((section) => section.groups.map((group) => (
                  <Section sheet label={<SectionLabel icon="folder" title={section.title ? `${section.title} · ${group.project}` : group.project} count={group.services.length} />}>
                    {group.services.map((service) => (
                      <Row
                        testID="service-row"
                        leading={<View class="h-9 min-w-[52px] items-center justify-center rounded-[11px] bg-background px-2"><Text class="font-mono text-code font-semibold">{String(service.port)}</Text></View>}
                        title={serviceProcessLabel(service)}
                        subtitle={!hasServiceTerminal(service) && service.source === "persistent" ? serviceSourceLabel(service) : serviceWorkerLabel(service)}
                        trailing={service.tunnel?.status === "running" ? <View class="rounded-full bg-background px-2.5 py-1"><Text class="text-caption font-semibold">Public</Text></View> : undefined}
                        chevron
                        onPress={() => selected.set(keyOf(service))}
                      />
                    ))}
                  </Section>
                )))}
                {loading() ? <Text class="mt-3 text-center text-caption text-muted-foreground">Refreshing</Text> : null}
              </View>
            </Scroll>
          ) : null}
        </View>
      )}
    </Sheet>
  );
}
