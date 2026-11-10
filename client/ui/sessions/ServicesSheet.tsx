import { cleanup, Clipboard, effect, openUrl, Scroll, signal, Text, untracked, View } from "nsh";
import { AlertDialog, Button, Sheet } from "nsh/base";
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
import { Icon, type IconName } from "../icons/Icon";
import { connection, current, currentTransport } from "../session";

/** Open from the Sessions list's menu. */
export const servicesOpen = signal(false);

const keyOf = (service: DiscoveredSessionService) => `${service.serverId}:${service.id}`;
const messageOf = (error: unknown, fallback: string) => (error instanceof Error && error.message ? error.message : fallback);
const capitalize = (value?: string) => (value ? value.charAt(0).toUpperCase() + value.slice(1) : "");

function open(url: string) {
  void openUrl(url).catch(() => {});
}

function Section(props: { title: string; footer?: string; children?: unknown }) {
  return (
    <View class="gap-1.5">
      <Text class="px-1 text-xs font-semibold uppercase text-muted-foreground">{props.title}</Text>
      <View class="overflow-hidden rounded-xl border border-border bg-card">{props.children as never}</View>
      {props.footer ? <Text class="px-1 text-xs text-muted-foreground">{props.footer}</Text> : null}
    </View>
  );
}

function Row(props: { testID?: string; icon?: IconName; title: string; subtitle?: string; value?: string; chevron?: boolean; destructive?: boolean; disabled?: boolean; onPress?: () => void }) {
  return (
    <Button testID={props.testID} onPress={props.onPress} disabled={props.disabled || !props.onPress}
      class={props.disabled ? "flex-row items-center gap-3 border-b border-border px-3 py-3 opacity-50" : "flex-row items-center gap-3 border-b border-border px-3 py-3 active:bg-accent"}>
      {props.icon ? <Icon name={props.icon} size={18} color={props.destructive ? "destructive" : "foreground"} /> : null}
      <View class="flex-1 gap-0.5">
        <Text class={props.destructive ? "text-base text-destructive" : "text-base"}>{props.title}</Text>
        {props.subtitle ? <Text class="text-xs text-muted-foreground line-clamp-2">{props.subtitle}</Text> : null}
      </View>
      {props.value ? <Text class="text-sm text-muted-foreground line-clamp-1">{props.value}</Text> : null}
      {props.chevron ? <Icon name="chevron-right" size={16} /> : null}
    </Button>
  );
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
    const { action } = await AlertDialog.show({
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
    <View class="gap-3">
      <View class="flex-row items-center gap-2">
        <Button testID="service-back" accessibilityLabel="Back to services" onPress={props.onBack} class="rounded-lg p-1 active:bg-accent">
          <Icon name="chevron-left" size={22} color="foreground" />
        </Button>
        <View class="flex-1">
          <Text class="text-lg font-semibold line-clamp-1">{serviceProcessLabel(props.service())}</Text>
          <Text class="text-xs text-muted-foreground line-clamp-1">{`Port ${props.service().port} · ${subtitle()}`}</Text>
        </View>
      </View>
      <Scroll style={{ maxHeight: 520 }}>
        <View class="gap-4 pb-2">
          <Section title="Open">
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
            <Section title="Public access" footer={active() ? undefined : "A temporary Quick Tunnel URL that anyone with the link can open."}>
              {publicURL() ? <Row testID="tunnel-open" icon="browser" title="Open public URL" subtitle={publicURL()} chevron onPress={() => open(publicURL()!)} /> : null}
              {publicURL() ? <Row testID="tunnel-copy" icon={copied() ? "check" : "copy"} title={copied() ? "Copied" : "Copy public URL"} onPress={() => void copy(publicURL()!)} /> : null}
              <Row testID="tunnel-toggle" icon={active() ? "stop-circle" : "share"} destructive={active()} disabled={busy()}
                title={busy() ? "Working…" : active() ? "Stop public tunnel" : "Share with Quick Tunnel"}
                subtitle={active() && !publicURL() ? capitalize(tunnel()?.status) : undefined}
                onPress={() => (untracked(active) ? void run("stop") : void start())} />
            </Section>
          ) : null}
          {problem() || tunnel()?.error ? (
            <View testID="tunnel-problem" class="gap-1 rounded-xl border border-border bg-card px-3 py-2.5">
              <Text class="text-sm font-semibold text-destructive">Tunnel unavailable</Text>
              <Text class="text-sm text-muted-foreground">{problem() || tunnel()?.error || ""}</Text>
            </View>
          ) : null}
          <Section title="Details">
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

  return (
    <Sheet open={servicesOpen()} onOpenChange={({ open }) => servicesOpen.set(open)} closeOnSwipe popupClass="rounded-t-2xl bg-background px-4 pt-4" backdropClass="bg-black/40">
      <View testID="services-sheet" class="pb-8">
        {selected() && selectedService() ? (
          <ServiceDetail service={() => selectedService()!} onOpenSession={(workerId) => {
            servicesOpen.set(false);
            props.onOpenSession(workerId);
          }} onBack={() => {
            selected.set("");
            void refresh();
          }} />
        ) : (
          <View class="gap-3">
            <View class="flex-row items-center gap-2">
              <View class="flex-1">
                <Text class="text-lg font-semibold">Services</Text>
                {subtitle() ? <Text class="text-xs text-muted-foreground">{subtitle()}</Text> : null}
              </View>
              <Button testID="services-refresh" accessibilityLabel="Refresh services" disabled={loading()} onPress={() => void refresh()} class="rounded-full p-1.5 active:bg-accent">
                <Icon name="refresh" size={20} color="foreground" />
              </Button>
              <Button testID="services-close" accessibilityLabel="Close services" onPress={() => servicesOpen.set(false)} class="rounded-full p-1.5 active:bg-accent">
                <Icon name="close" size={20} color="foreground" />
              </Button>
            </View>
            {problem() ? (
              <View testID="services-problem" class="gap-2 rounded-xl border border-border bg-card px-3 py-3">
                <Text class="text-sm font-semibold text-destructive">Services could not be loaded</Text>
                <Text class="text-sm text-muted-foreground">{problem()}</Text>
                <Button testID="services-retry" disabled={loading()} onPress={() => void refresh()} class="self-start rounded-full border border-border px-3 py-1.5 active:bg-accent">
                  <Text class="text-sm font-semibold text-primary">Retry</Text>
                </Button>
              </View>
            ) : null}
            {count() === 0 && !problem() ? (
              <View testID="services-empty" class="items-center gap-2 py-8">
                <Icon name="radio" size={28} />
                <Text class="text-base font-semibold">{loading() ? "Loading services" : "No listening services"}</Text>
                {loading() ? null : <Text class="text-center text-sm text-muted-foreground">Ports opened by Sessions and persistent services appear here.</Text>}
              </View>
            ) : null}
            {count() > 0 ? (
              <Scroll style={{ maxHeight: 520 }}>
                <View class="gap-4 pb-2">
                  {groupSessionServices(services()).flatMap((section) => section.groups.map((group) => (
                    <Section title={section.title ? `${section.title} · ${group.project}` : group.project}>
                      {group.services.map((service) => (
                        <Button testID="service-row" onPress={() => selected.set(keyOf(service))} class="flex-row items-center gap-3 border-b border-border px-3 py-2.5 active:bg-accent">
                          <View class="min-w-[46px] items-center rounded-lg bg-accent px-1.5 py-1">
                            <Text class="font-mono text-sm text-primary">{String(service.port)}</Text>
                          </View>
                          <View class="flex-1 gap-0.5">
                            <Text class="text-base line-clamp-1">{serviceProcessLabel(service)}</Text>
                            <Text class="text-xs text-muted-foreground line-clamp-1">
                              {!hasServiceTerminal(service) && service.source === "persistent" ? serviceSourceLabel(service) : serviceWorkerLabel(service)}
                            </Text>
                          </View>
                          {service.tunnel?.status === "running" ? <Text class="rounded-full bg-accent px-2 py-0.5 text-xs text-primary">Public</Text> : null}
                          <Icon name="chevron-right" size={16} />
                        </Button>
                      ))}
                    </Section>
                  )))}
                  {loading() ? <Text class="text-center text-xs text-muted-foreground">Refreshing</Text> : null}
                </View>
              </Scroll>
            ) : null}
          </View>
        )}
      </View>
    </Sheet>
  );
}
