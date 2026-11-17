import { cleanup, computed, effect, signal, Text, untracked, View, type JSX, lifecycle } from "nsh";
import { Button } from "nsh/base";
import { floatPlaceholder, phoneMode } from "../../core/phone/setup";
import {
  displayShown,
  displaySource,
  floatSources,
  macSource,
  phoneSource,
  pickSource,
  screenTarget,
  type FloatPage,
  type ScreenSource,
} from "../../core/screen/float";
import { haltDisplay, listDisplays, screenRequest, ScreenRequestError, type ScreenAgent } from "../../core/screen/screen";
import { floatedScreenAgents } from "../../core/screen/screenPageModel";
import { isWorkerActivelyRunning } from "../../core/sessions/workerStatusPresentation";
import type { StoredServer } from "../../core/storedServerContract";
import { Icon } from "../kit/Icon";
import { Spinner } from "../kit/kit";
import { LINUX_MODIFIERS, MAC_MODIFIERS, RemoteSession, RemoteView } from "../more/screen/RemoteView";
import { PhoneLiveView } from "../phone/contract";
import { phoneAvailable, phoneControl, phoneStatus } from "../phone/state";
import { current, workers } from "../session";
import { ScreenFloat, type FloatMenuItem } from "./ScreenFloat";
import { agentDisplays } from "./agentDisplays";

/** How often the current computer is asked which agents use a screen, while one might. */
const POLL_MS = 3_000;
/** How often, while nothing runs, while a display an agent left (a sign-in page for the person) is still open. */
const LEFT_OPEN_POLL_MS = 15_000;
/** Frames the phone's small window asks for, and the width in steps so a resize does not ask again at every move. */
const PHONE_FPS = 4;
const FRAME_STEP = 120;
const FRAME_MIN = 240;
const FRAME_MAX = 720;

export interface ScreenFloatHostProps {
  /** The page the person is on: whose it is, and whether it is Screen or the Phone page (on screen, not just mounted under a hidden tab). */
  page: () => FloatPage;
  onOpenPhone: () => void;
  /** Opens Screen (for a display, its Worker's; `takeOver` takes it over as it opens). */
  onOpenScreen: (worker: string | null, takeOver?: boolean) => void;
  /** What lies under the window's area at the bottom (the tab bar). */
  bottomInset?: () => number;
  /** What lies left of it (the wide layout's left pane). */
  leftInset?: () => number;
}

/**
 * The floating live view of whichever agent screen the person is near: while the agent of the Session or
 * Work file on screen uses the phone, its Linux display or the Mac, it floats over the page. Mounted once
 * above the app's sections; each screen's own page is the full view and hides it there. From the Expo
 * app's ScreenFloatHost.
 */
export function ScreenFloatHost(props: ScreenFloatHostProps) {
  const displays = agentDisplays;
  displays.set([]);
  const mac = signal<ScreenAgent[]>([]);
  const picked = signal<string | null>(null);
  const tick = signal(0);
  const refresh = () => tick.set(tick() + 1);
  // A display that was up stays up between the agent's actions while its Worker runs.
  let shown = new Set<string>();
  let missing = { server: "", display: false, mac: false };
  /** What last made the reads below start now: a display staying open only changes how often they repeat. */
  let lastWake = "";

  // A computed: only whether something runs. A Session's own news (its activity, its output, several a second
  // while Workers run) must not run the reads below again: each one signs two requests on the JS thread, and
  // Brain's thread stuttered under them (owner 11-13).
  const running = computed(() => workers().some((worker) => isWorkerActivelyRunning(worker.status)));
  const mayUse = () => running();
  // A display outlives its agent's turn (a page left for the person); while one is open it is read now and then.
  const leftOpen = computed(() => displays().length > 0);

  // A new computer: nothing to show until the next read. Nothing that could use a screen: no Mac agents.
  let reset = "";
  let resetServer = "";
  effect(() => {
    const server = current()?.id ?? "";
    const key = `${server}:${mayUse()}`;
    untracked(() => {
      if (key === reset) return;
      reset = key;
      mac.set([]);
      if (server !== resetServer) displays.set([]);
      resetServer = server;
    });
  });

  // Asks while something could be using a screen and the app is in front: often while an agent runs, now and then while
  // a display it left is open, and once otherwise (coming to the front, a new computer), so a page left for the person
  // shows on its Work card. Never asks an endpoint the computer has said it lacks.
  effect(() => {
    const server = current();
    const active = lifecycle() === "active";
    const enabled = mayUse();
    const lingering = leftOpen();
    tick();
    if (!server || !active) {
      lastWake = ""; // coming back to the front reads again
      return;
    }
    let stopped = false;
    if (missing.server !== server.id) missing = { server: server.id, display: false, mac: false };
    const read = async () => {
      const gone = missing;
      const [shownNow, status] = await Promise.all([
        gone.display
          ? null
          : listDisplays(server).then(
              (result) => {
                if (!result.available) gone.display = true;
                return result.displays;
              },
              (error) => {
                if (error instanceof ScreenRequestError && (error.status === 404 || error.status === 503)) gone.display = true;
                return null;
              },
            ),
        gone.mac || !enabled
          ? null
          : screenRequest(server).then(
              (result) => {
                if (!result.supported) gone.mac = true;
                return result;
              },
              () => null,
            ),
      ]);
      if (stopped || current()?.id !== server.id) return;
      if (shownNow) displays.set(shownNow);
      if (status) mac.set(floatedScreenAgents(status));
    };
    const wake = `${server.id}:${enabled}:${tick()}`;
    if (wake !== lastWake) void read();
    lastWake = wake;
    const every = enabled ? POLL_MS : lingering ? LEFT_OPEN_POLL_MS : 0;
    const timer = every ? setInterval(() => void read(), every) : undefined;
    return () => {
      stopped = true;
      if (timer) clearInterval(timer);
    };
  });

  const all = computed<ScreenSource[]>(() => {
    const list = workers();
    const out: ScreenSource[] = [];
    const phone = phoneAvailable() ? phoneSource(phoneStatus(), list) : null;
    if (phone) out.push(phone);
    for (const display of displays()) {
      const worker = list.find((candidate) => candidate.id === display.id);
      const key = `display:${display.id}`;
      if (displayShown(display, { running: worker ? isWorkerActivelyRunning(worker.status) : false, shown: shown.has(key), holding: false })) {
        out.push(displaySource(display, worker?.name));
      }
    }
    for (const agent of mac()) out.push(macSource({ ...agent, name: agent.name || list.find((candidate) => candidate.id === agent.worker)?.name }));
    return out;
  });
  const sources = computed(() => floatSources(all(), props.page()));
  const source = () => pickSource(sources(), picked());
  effect(() => {
    const keys = sources().filter((s) => s.kind === "display").map((s) => s.key);
    untracked(() => {
      shown = new Set(keys);
    });
  });

  const cycle = () => {
    const list = sources();
    const index = list.findIndex((s) => s.key === source()?.key);
    if (list.length > 0) picked.set(list[(index + 1) % list.length].key);
  };
  const shared = (key: string) => {
    const index = () => Math.max(0, sources().findIndex((s) => s.key === key));
    return { count: () => sources().length, index, onCycle: cycle };
  };

  // One window per screen and computer: what changes in it redraws in place; another screen is a new window.
  const content = (key: string): JSX.Element => {
    const server = current();
    const first = untracked(source);
    if (!server || !first) return <View />;
    const live = () => sources().find((s) => s.key === first.key) ?? first;
    if (first.kind === "phone") return <PhoneContent source={live} {...shared(first.key)} onOpen={props.onOpenPhone} bottomInset={props.bottomInset} leftInset={props.leftInset} />;
    return (
      <RemoteContent
        server={server}
        source={live}
        {...shared(first.key)}
        onOpen={() => props.onOpenScreen(first.kind === "display" ? first.owner : null)}
        onTakeOver={() => props.onOpenScreen(first.kind === "display" ? first.owner : null, first.kind === "display")}
        onChanged={refresh}
        bottomInset={props.bottomInset}
        leftInset={props.leftInset}
        key={key}
      />
    );
  };
  // A computed, so a poll that changes nothing about which screen it is never rebuilds the window under a finger.
  const windowKey = computed(() => {
    const s = source();
    return s ? `${current()?.id ?? ""}|${s.key}` : "";
  });
  return <>{((key) => untracked(() => (key ? content(key) : null)))(windowKey())}</>;
}

interface FloatProps {
  source: () => ScreenSource;
  count: () => number;
  index: () => number;
  onCycle: () => void;
  onOpen: () => void;
  bottomInset?: () => number;
  leftInset?: () => number;
}

/** Phone use: the native live view, watch only. Taking over is in the Phone page, which Take over opens. */
function PhoneContent(props: FloatProps) {
  const mode = () => (phoneStatus() ? phoneMode(phoneStatus()!) : "idle");
  const placeholder = () => (phoneStatus() ? floatPlaceholder(phoneStatus()!) : null);
  const menu = (): FloatMenuItem[] => [
    mode() === "handoff"
      ? { id: "done", label: "Done", run: () => phoneControl("handoffDone") }
      : mode() === "driving"
        ? { id: "give-back", label: "Give back", run: () => phoneControl("giveBack") }
        : {
            id: "take-over",
            label: "Take over",
            run: () => {
              phoneControl("takeOver");
              props.onOpen();
            },
          },
    { id: "stop", label: "Stop", destructive: true, run: () => phoneControl("stop") },
  ];
  return (
    <ScreenFloat
      ratio={() => props.source().width / props.source().height}
      title={() => props.source().title}
      count={props.count}
      index={props.index}
      onCycle={props.onCycle}
      onOpen={props.onOpen}
      menu={menu}
      attention={() => mode() === "handoff"}
      bottomInset={props.bottomInset}
      leftInset={props.leftInset}
    >
      {(picture) => (
        <View class="flex-1">
          <View class="absolute inset-0" style={{ pointerEvents: "none" }}>
            <PhoneLiveView class="flex-1" fps={PHONE_FPS} maxWidth={Math.min(FRAME_MAX, Math.max(FRAME_MIN, Math.ceil((picture().width * 3) / FRAME_STEP) * FRAME_STEP))} />
          </View>
          <If when={placeholder()}><View testID="screen-float-placeholder" class="absolute inset-0 items-center justify-center gap-2 bg-card p-3" style={{ pointerEvents: "none" }}>
              <If when={!placeholder()!.failed}><Spinner size={18} tone="faint" /></If>
              <If when={placeholder()!.text}><Text class={`text-center text-xs ${placeholder()!.failed ? "text-destructive" : "text-muted-foreground"}`}>{placeholder()!.text!}</Text></If>
            </View></If>
        </View>
      )}
    </ScreenFloat>
  );
}

interface RemoteContentProps extends FloatProps {
  server: StoredServer;
  /** Opens the screen's stage taken over (a display), or the stage (the Mac's, where Take over is). */
  onTakeOver: () => void;
  /** The agent's display or control changed here: read the computer again. */
  onChanged: () => void;
  key?: string;
}

/**
 * A Linux display or the Mac's screen, to watch: the same live picture as Screen's stage, too small to use.
 * A tap opens the stage; Take over in the menu opens it taken over.
 */
function RemoteContent(props: RemoteContentProps) {
  const first = untracked(props.source);
  const target = screenTarget(first);
  const display = first.kind === "display";
  const session = new RemoteSession(props.server, { watch: target?.watch, worker: target?.worker, route: target?.route, align: "center" });
  session.connect();
  // A phone that leaves the app gives its viewers up; coming back reconnects them.
  effect(() => {
    const active = lifecycle() === "active";
    untracked(() => {
      if (!active) session.lose(null);
      else if (session.phase() === "lost") session.connect();
    });
  });
  cleanup(() => session.disconnect());

  const stop = async () => {
    try {
      if (display) await haltDisplay(props.server, first.owner ?? "");
      else await screenRequest(props.server, { action: "stop_agent", worker: first.owner ?? undefined });
    } catch {
      // The next read shows what the computer says.
    }
    props.onChanged();
  };
  const menu = (): FloatMenuItem[] => [
    { id: "take-over", label: "Take over", run: props.onTakeOver },
    { id: "stop", label: "Stop", destructive: true, run: () => void stop() },
    ...(session.phase() === "lost" ? [{ id: "reconnect", label: "Reconnect", run: () => session.connect() }] : []),
  ];
  return (
    <ScreenFloat
      ratio={() => props.source().width / props.source().height}
      title={() => props.source().title}
      count={props.count}
      index={props.index}
      onCycle={props.onCycle}
      onOpen={props.onOpen}
      menu={menu}
      bottomInset={props.bottomInset}
      leftInset={props.leftInset}
    >
      {(picture) => (
        <View class="flex-1">
          <View style={{ width: picture().width, height: picture().height, pointerEvents: "none" }}>
            <RemoteView session={session} label={`${display ? "Display" : "Screen"} of ${props.source().title}`} modifiers={display ? LINUX_MODIFIERS : MAC_MODIFIERS} typing={false} onTypingChange={() => {}} class="flex-1" />
          </View>
          {/* The picture is for watching: a tap opens the stage. */}
          <Button testID="screen-float-picture" accessibilityLabel={`Open ${props.source().title}`} onPress={props.onOpen} class="absolute inset-x-0 top-0" style={{ height: picture().height }} />
          <If when={session.phase() !== "live"}><View testID="screen-float-cover" class="absolute inset-x-0 top-0 items-center justify-center" style={{ height: picture().height, pointerEvents: session.phase() === "lost" ? "box-none" : "none" }}>
              <If when={session.phase() === "connecting"} else={<Button testID="screen-float-reconnect" accessibilityLabel="Reconnect" onPress={() => session.connect()} class="size-12 items-center justify-center rounded-full bg-secondary">
                  <Icon name="refresh" size={22} />
                </Button>}><Spinner size={20} tone="faint" /></If>
            </View></If>
        </View>
      )}
    </ScreenFloat>
  );
}
