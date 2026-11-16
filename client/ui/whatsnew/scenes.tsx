import { createMotion, Easing, Text, View } from "nsh";
import type { JSX, MotionFrame } from "nsh";
import { Icon, type IconName } from "../kit/Icon";

/**
 * What's New's pictures: each highlight's feature in use, drawn as a small loop of the app's own parts (a chat card,
 * a Session row, a browser window), so it follows the theme, works offline and weighs nothing. A release names one
 * by `media.scene` (release-notes/<version>.json); a name this app does not know draws its glyph alone.
 *
 * Every loop rests on its telling frame: with reduced motion nothing moves and that frame is what shows.
 */

/** One loop's length: slow enough to read, short enough to see it twice on a page. */
const LOOP = 3600;
const EASE = Easing.bezier(0.2, 0.8, 0.2, 1);

function loop(frames: MotionFrame[], duration = LOOP) {
  return createMotion({ enter: { keyframes: frames, duration, easing: EASE, iterations: "infinite" } }).style;
}

/** In at `from`, held, out just before the loop starts again; at rest it shows. */
const appear = (from: number, rise = 6): MotionFrame[] => [
  { offset: 0, opacity: 0, translateY: rise },
  { offset: from, opacity: 0, translateY: rise },
  { offset: Math.min(from + 0.1, 0.9), opacity: 1, translateY: 0 },
  { offset: 0.9, opacity: 1, translateY: 0 },
  { offset: 1, opacity: 0, translateY: 0 },
];

/** Shown only between `from` and `to` (a state the loop passes through); at rest hidden. */
const between = (from: number, to: number): MotionFrame[] => [
  { offset: 0, opacity: 0 },
  { offset: from, opacity: 0 },
  { offset: from + 0.06, opacity: 1 },
  { offset: to, opacity: 1 },
  { offset: Math.min(to + 0.06, 1), opacity: 0 },
  { offset: 1, opacity: 0 },
];

/** A press: a ring that swells and fades where the person taps. */
function Press(props: { at: number; style?: Record<string, unknown> }) {
  const style = loop([
    { offset: 0, opacity: 0, scale: 0.6 },
    { offset: props.at, opacity: 0, scale: 0.6 },
    { offset: props.at + 0.04, opacity: 0.9, scale: 1 },
    { offset: props.at + 0.14, opacity: 0, scale: 1.5 },
    { offset: 1, opacity: 0, scale: 1.5 },
  ]);
  return <View class="absolute size-[26px] rounded-full border-2 border-foreground" style={{ ...props.style, ...style, opacity: 0, pointerEvents: "none" }} />;
}

function Line(props: { width: number | string; strong?: boolean }) {
  return <View class={`h-[7px] rounded-full ${props.strong ? "bg-foreground opacity-80" : "bg-track"}`} style={{ width: props.width as number }} />;
}

function Dot(props: { tone?: "ok" | "seal" | "soft" }) {
  const fill = props.tone === "ok" ? "bg-add" : props.tone === "seal" ? "bg-seal" : "bg-faint";
  return <View class={`size-[7px] rounded-full ${fill}`} />;
}

function Card(props: { class?: string; children?: JSX.Element; style?: Record<string, unknown> }) {
  return <View class={`rounded-[16px] bg-card ${props.class ?? ""}`} style={props.style}>{props.children}</View>;
}

/** A Session's row: its glyph, name and state. */
function SessionRow(props: { name: string; state: string }) {
  return (
    <View class="flex-row items-center gap-2 px-3 py-2">
      <Icon name="terminal" size={14} tone="soft" />
      <Text class="flex-1 text-[12px] font-semibold text-foreground">{props.name}</Text>
      <Dot tone="ok" />
      <Text class="text-[11px] text-muted-foreground">{props.state}</Text>
    </View>
  );
}

/** A browser window: its bar and a page of lines. */
function Browser(props: { children?: JSX.Element; label?: string }) {
  return (
    <Card class="overflow-hidden">
      <View class="flex-row items-center gap-1.5 border-b border-border px-3 py-2">
        <View class="size-[7px] rounded-full bg-track" />
        <View class="size-[7px] rounded-full bg-track" />
        <View class="ml-1 h-[14px] flex-1 flex-row items-center rounded-full bg-muted px-2">
          <Text class="text-[9px] text-muted-foreground line-clamp-1">{props.label ?? "app.example.com"}</Text>
        </View>
      </View>
      <View class="gap-2 p-3">{props.children}</View>
    </Card>
  );
}

/** Claude's question in Chat, answered with a tap. */
function ChatCard() {
  const card = loop(appear(0.04, 10));
  const chosen = loop([{ offset: 0, opacity: 0 }, { offset: 0.42, opacity: 0 }, { offset: 0.48, opacity: 1 }, { offset: 1, opacity: 1 }]);
  return (
    <View class="w-[250px] gap-2">
      <View class="self-end rounded-[14px] bg-accent px-3 py-1.5"><Text class="text-[12px] text-foreground">Start in ~/app</Text></View>
      <Card class="gap-1 p-2" style={card}>
        <View class="px-2 pb-1 pt-1"><Text class="text-[13px] font-semibold text-foreground">Trust this folder?</Text></View>
        <View class="relative flex-row items-center gap-2 rounded-[10px] px-2 py-2">
          <View class="absolute inset-0 rounded-[10px] bg-accent" style={chosen} />
          <Text class="flex-1 text-[12px] text-foreground">Yes, I trust this folder</Text>
          <View style={chosen}><Icon name="check" size={14} tone="ink" /></View>
          <Press at={0.4} style={{ right: 18, top: 4 }} />
        </View>
        <View class="flex-row items-center rounded-[10px] px-2 py-2">
          <Text class="flex-1 text-[12px] text-muted-foreground">No, exit</Text>
        </View>
      </Card>
    </View>
  );
}

/** Mewla restarts onto the new version; the Sessions never stop. */
function UpdateRestart(props: { version: string }) {
  const spin = createMotion({ enter: { keyframes: [{ offset: 0, rotate: 0 }, { offset: 0.5, rotate: 360 }, { offset: 1, rotate: 360 }], duration: LOOP, easing: EASE, iterations: "infinite" } }).style;
  const updating = loop(between(0.02, 0.5));
  const done = loop([{ offset: 0, opacity: 1 }, { offset: 0.02, opacity: 0 }, { offset: 0.52, opacity: 0 }, { offset: 0.6, opacity: 1 }, { offset: 1, opacity: 1 }]);
  return (
    <View class="w-[250px] gap-2">
      <Card class="flex-row items-center gap-2 px-3 py-2.5">
        <View style={spin}><Icon name="sync" size={16} tone="ink" /></View>
        <Text class="flex-1 text-[13px] font-semibold text-foreground">Mewla</Text>
        <View class="relative h-[18px] min-w-[86px] items-end justify-center">
          <Text class="absolute text-[11px] text-muted-foreground" style={updating}>Restarting</Text>
          <Text class="absolute text-[11px] font-semibold text-foreground" style={done}>{props.version}</Text>
        </View>
      </Card>
      <Card>
        <SessionRow name="Claude" state="Running" />
        <View class="mx-3 h-px bg-border" />
        <SessionRow name="Codex" state="Running" />
      </Card>
    </View>
  );
}

/** An agent's browser window in Screen, live: a page fills in, the person holds it. */
function AgentWindows() {
  const lines = [0.12, 0.22, 0.32].map((at) => loop(appear(at, 0)));
  const held = loop([{ offset: 0, opacity: 0 }, { offset: 0.56, opacity: 0 }, { offset: 0.62, opacity: 1 }, { offset: 1, opacity: 1 }]);
  return (
    <View class="w-[260px] gap-2">
      <View class="flex-row items-center gap-2">
        <View class="rounded-full bg-primary px-3 py-1"><Text class="text-[11px] font-semibold text-primary-foreground">Worker</Text></View>
        <View class="rounded-full bg-card px-3 py-1"><Text class="text-[11px] text-muted-foreground">This screen</Text></View>
        <View class="flex-1" />
        <View class="flex-row items-center gap-1 rounded-full bg-card px-2.5 py-1" style={held}>
          <Icon name="hand" size={12} tone="ink" />
          <Text class="text-[11px] font-semibold text-foreground">Held</Text>
        </View>
      </View>
      <View class="relative">
        <Browser label="accounts.example.com">
          <Line width={120} strong />
          <View style={lines[0]}><Line width={190} /></View>
          <View style={lines[1]}><Line width={160} /></View>
          <View class="mt-1 h-[22px] w-[90px] items-center justify-center rounded-[8px] bg-primary" style={lines[2]}>
            <Text class="text-[10px] font-semibold text-primary-foreground">Sign in</Text>
          </View>
        </Browser>
        <Press at={0.5} style={{ left: 40, bottom: 6 }} />
      </View>
    </View>
  );
}

/** Mewla restarts under an open page; the page stays. */
function BrowserSurvives(props: { version: string }) {
  const updating = loop(between(0.15, 0.5));
  const kept = loop([{ offset: 0, opacity: 0 }, { offset: 0.56, opacity: 0, translateY: 4 }, { offset: 0.64, opacity: 1, translateY: 0 }, { offset: 1, opacity: 1 }]);
  return (
    <View class="relative w-[260px] pb-3">
      <Browser label="dashboard.example.com">
        <Line width={110} strong />
        <View class="flex-row gap-2">
          <View class="h-[44px] flex-1 rounded-[10px] bg-muted" />
          <View class="h-[44px] flex-1 rounded-[10px] bg-muted" />
        </View>
        <Line width={180} />
        <Line width={140} />
      </Browser>
      <View class="absolute bottom-0 left-0 right-0 items-center">
        <View class="relative h-[24px] w-[180px] items-center">
          <View class="absolute flex-row items-center gap-1 rounded-full bg-muted px-3 py-1" style={updating}>
            <Icon name="sync" size={12} tone="soft" />
            <Text class="text-[11px] text-muted-foreground">Updating Mewla</Text>
          </View>
          <View class="absolute flex-row items-center gap-1 rounded-full bg-primary px-3 py-1" style={kept}>
            <Icon name="check" size={12} tone="canvas" />
            <Text class="text-[11px] font-semibold text-primary-foreground">{`${props.version} · page kept`}</Text>
          </View>
        </View>
      </View>
    </View>
  );
}

/** The phone walks from Wi-Fi to mobile data; Brain keeps answering. */
function NetworkHop() {
  const wifi = loop([{ offset: 0, opacity: 1 }, { offset: 0.4, opacity: 1 }, { offset: 0.48, opacity: 0 }, { offset: 0.92, opacity: 0 }, { offset: 1, opacity: 1 }]);
  const cell = loop([{ offset: 0, opacity: 0 }, { offset: 0.4, opacity: 0 }, { offset: 0.48, opacity: 1 }, { offset: 0.92, opacity: 1 }, { offset: 1, opacity: 0 }]);
  const pulse = loop([{ offset: 0, translateX: -70, opacity: 0 }, { offset: 0.1, opacity: 1 }, { offset: 0.45, translateX: 70, opacity: 1 }, { offset: 0.5, translateX: 70, opacity: 0 }, { offset: 0.55, translateX: -70, opacity: 0 }, { offset: 0.6, opacity: 1 }, { offset: 0.95, translateX: 70, opacity: 1 }, { offset: 1, translateX: 70, opacity: 0 }]);
  const reply = loop(appear(0.58));
  return (
    <View class="w-[260px] gap-3">
      <View class="flex-row items-center gap-2">
        <View class="relative h-[26px] w-[104px]">
          <View class="absolute inset-0 flex-row items-center justify-center gap-1 rounded-full bg-card" style={wifi}>
            <Icon name="radio" size={12} tone="ink" />
            <Text class="text-[11px] font-semibold text-foreground">Wi-Fi</Text>
          </View>
          <View class="absolute inset-0 flex-row items-center justify-center gap-1 rounded-full bg-card" style={cell}>
            <Icon name="pulse" size={12} tone="ink" />
            <Text class="text-[11px] font-semibold text-foreground">Mobile data</Text>
          </View>
        </View>
        <View class="h-[2px] flex-1 overflow-hidden rounded-full bg-track">
          <View class="h-[2px] w-[28px] self-center rounded-full bg-foreground" style={pulse} />
        </View>
        <View class="size-[26px] items-center justify-center rounded-full bg-card"><Icon name="desktop" size={13} tone="ink" /></View>
      </View>
      <View class="flex-row items-center gap-1.5 self-center rounded-full bg-card px-3 py-1">
        <Dot tone="ok" />
        <Text class="text-[11px] text-foreground">Connected</Text>
      </View>
      <View class="gap-2">
        <View class="self-end rounded-[14px] bg-accent px-3 py-1.5"><Text class="text-[12px] text-foreground">How's the build?</Text></View>
        <View class="self-start" style={reply}><Text class="text-[12px] text-foreground">Green. Shipping it now.</Text></View>
      </View>
    </View>
  );
}

/** An update leaves its line in Brain's chat; a tap opens what's new. */
function UpdateLine(props: { version: string }) {
  const line = loop(appear(0.12, 0));
  const sheet = loop([{ offset: 0, opacity: 0, translateY: 40 }, { offset: 0.58, opacity: 0, translateY: 40 }, { offset: 0.68, opacity: 1, translateY: 0 }, { offset: 0.9, opacity: 1, translateY: 0 }, { offset: 1, opacity: 0, translateY: 40 }]);
  return (
    <View class="relative h-[150px] w-[250px] justify-center gap-2.5 overflow-hidden">
      <View class="self-end rounded-[14px] bg-accent px-3 py-1.5"><Text class="text-[12px] text-foreground">Morning</Text></View>
      <View class="relative flex-row items-center justify-center gap-1" style={line}>
        <Text class="text-[11px] text-muted-foreground">{`Updated to ${props.version} · `}</Text>
        <Text class="text-[11px] font-semibold text-foreground">What's new</Text>
        <Press at={0.48} style={{ right: 52, top: -6 }} />
      </View>
      <View class="self-start"><Text class="text-[12px] text-foreground">Morning! Two things are new.</Text></View>
      <View class="absolute bottom-0 left-4 right-4 h-[44px] rounded-t-[16px] bg-card px-4 pt-2" style={sheet}>
        <View class="h-[4px] w-[28px] self-center rounded-full bg-track" />
        <View class="mt-2"><Line width={120} strong /></View>
      </View>
    </View>
  );
}

type SceneProps = { version: string; target?: string };

const SCENES: Record<string, (props: SceneProps) => JSX.Element> = {
  "chat-card": ChatCard,
  "update-restart": UpdateRestart,
  "agent-windows": AgentWindows,
  "browser-survives": BrowserSurvives,
  "network-hop": NetworkHop,
  "update-line": UpdateLine,
};

/** Whether this app draws the scene (release notes name one; a test holds every bundled name to this). */
export function knowsScene(name: string): boolean {
  return name in SCENES;
}

/** The glyph a scene this app does not know falls back to (a computer's newer notes): where its Try it goes. */
const FALLBACK: Record<string, IconName> = {
  screen: "desktop",
  sessions: "sessions",
  "new-session": "add-circle",
  computer: "server",
  calendar: "calendar",
  settings: "settings",
  brain: "brain",
};

function Glyph(props: SceneProps) {
  return <Icon name={FALLBACK[props.target ?? ""] ?? "bulb"} size={44} tone="ink" />;
}

/** A page's picture, in its frame. */
export function Scene(props: { name: string; version: string; target?: string }) {
  const Drawn = SCENES[props.name] ?? Glyph;
  return (
    <View testID={`whats-new-scene-${props.name}`} accessibilityElementsHidden class="h-[204px] items-center justify-center overflow-hidden rounded-[22px] bg-sheet-surface">
      {/* Drawn at the size of the app's own parts and shown a little larger, so a phone reads them. */}
      <View style={{ transform: [{ scale: 1.16 }] }}><Drawn version={props.version} target={props.target} /></View>
    </View>
  );
}
