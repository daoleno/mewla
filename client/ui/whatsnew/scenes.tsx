import { createMotion, Easing, reduceMotion, signal, Text, untracked, View } from "nsh";
import type { JSX, MotionFrame } from "nsh";
import { color, withAlpha } from "../kit/colors";
import { Icon, type IconName } from "../kit/Icon";
import { ProgressHalo } from "../kit/ProgressHalo";

/**
 * What's New's pictures: each highlight's feature in use, drawn as a small loop of the app's own parts (a chat card,
 * a Session row, a browser window), so it follows the theme, works offline and weighs nothing. A release names one
 * by `media.scene` (release-notes/<version>.json); a name this app does not know draws its glyph alone (the computer's
 * notes carry no highlights since 0.4.20, so only an app's own notes name its scenes).
 *
 * Every loop rests on its telling frame: with reduced motion nothing moves and that frame is what shows.
 */

/** One loop's length: slow enough to read, short enough to see it twice on a page. */
const LOOP = 3600;
const EASE = Easing.bezier(0.2, 0.8, 0.2, 1);

/** Where a loop is still under reduced motion: past every passing state, on the telling one. */
const TELLING = 0.85;

/** Each channel of `frames` at `offset`, straight between the frames around it. */
function sample(frames: MotionFrame[], offset: number): Record<string, number> {
  const out: Record<string, number> = {};
  const channels = new Set(frames.flatMap((frame) => Object.keys(frame).filter((key) => key !== "offset" && key !== "easing")));
  for (const channel of channels) {
    const points = frames
      .map((frame, index) => ({ at: frame.offset ?? index / Math.max(1, frames.length - 1), value: (frame as Record<string, unknown>)[channel] }))
      .filter((point): point is { at: number; value: number } => typeof point.value === "number");
    let value = points[0]?.value ?? 0;
    for (let i = 0; i < points.length; i += 1) {
      if (points[i].at <= offset) value = points[i].value;
      else {
        const before = points[i - 1];
        if (before) value = before.value + ((points[i].value - before.value) * (offset - before.at)) / (points[i].at - before.at);
        break;
      }
    }
    out[channel] = value;
  }
  return out;
}

/**
 * A loop on a view. With reduced motion it stands still on its telling frame: NSH would land it on its last frame,
 * which for most of these is the blank one before the loop starts again.
 */
function loop(frames: MotionFrame[], duration = LOOP): Record<string, unknown> {
  if (untracked(reduceMotion)) {
    const v = sample(frames, TELLING);
    return {
      opacity: v.opacity ?? 1,
      transform: [{ translateX: v.translateX ?? 0 }, { translateY: v.translateY ?? 0 }, { scale: v.scale ?? 1 }, { rotate: `${v.rotate ?? 0}deg` }],
    };
  }
  return createMotion({ enter: { keyframes: frames, duration, easing: EASE, iterations: "infinite" } }).style as Record<string, unknown>;
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
  const spin = loop([{ offset: 0, rotate: 0 }, { offset: 0.5, rotate: 360 }, { offset: 1, rotate: 360 }]);
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

/** A reply draws its flowchart, and the file it names opens with a tap. */
function ChatDiagram() {
  const boxes = [0.08, 0.2, 0.32].map((at) => loop(appear(at, 4)));
  const arrows = [0.16, 0.28].map((at) => loop(appear(at, 0)));
  const preview = loop([{ offset: 0, opacity: 0, translateY: 30 }, { offset: 0.6, opacity: 0, translateY: 30 }, { offset: 0.7, opacity: 1, translateY: 0 }, { offset: 0.92, opacity: 1, translateY: 0 }, { offset: 1, opacity: 0, translateY: 30 }]);
  const Box = (props: { label: string; style: Record<string, unknown>; strong?: boolean }) => (
    <View class={`items-center justify-center rounded-[8px] px-2.5 py-1.5 ${props.strong ? "bg-primary" : "bg-card"}`} style={props.style}>
      <Text class={`text-[11px] font-semibold ${props.strong ? "text-primary-foreground" : "text-foreground"}`}>{props.label}</Text>
    </View>
  );
  const Arrow = (props: { style: Record<string, unknown> }) => (
    <View class="flex-row items-center" style={props.style}>
      <View class="h-[2px] w-[14px] bg-faint" />
      <Icon name="chevron-right" size={11} tone="faint" />
    </View>
  );
  return (
    <View class="relative h-[150px] w-[260px] justify-center gap-2.5 overflow-hidden">
      <View class="flex-row items-center justify-center gap-1 rounded-[14px] bg-muted px-2 py-3">
        <Box label="Push" style={boxes[0]} />
        <Arrow style={arrows[0]} />
        <Box label="Build" style={boxes[1]} />
        <Arrow style={arrows[1]} />
        <Box label="Ship" strong style={boxes[2]} />
      </View>
      <View class="relative flex-row items-center gap-1 self-start">
        <Text class="text-[12px] text-foreground">Steps are in</Text>
        <Text class="text-[12px] font-semibold text-foreground underline">docs/release.md</Text>
        <Press at={0.5} style={{ right: 30, top: -5 }} />
      </View>
      <View class="absolute bottom-0 left-3 right-3 h-[52px] rounded-t-[16px] border border-b-0 border-border bg-muted px-3 pt-2" style={preview}>
        <View class="flex-row items-center gap-1.5">
          <Icon name="document-text" size={13} tone="ink" />
          <Text class="text-[11px] font-semibold text-foreground">release.md</Text>
        </View>
        <View class="mt-2 gap-1.5"><Line width={170} /></View>
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

/** Screen's stage: a desktop page too small to use; Take over lays it out for the phone, in the seal's red frame. */
function ScreenReflow() {
  const desktop = loop([{ offset: 0, opacity: 1 }, { offset: 0.4, opacity: 1 }, { offset: 0.46, opacity: 0 }, { offset: 1, opacity: 0 }]);
  const phone = loop([{ offset: 0, opacity: 0 }, { offset: 0.42, opacity: 0, scale: 0.96 }, { offset: 0.5, opacity: 1, scale: 1 }, { offset: 1, opacity: 1, scale: 1 }]);
  const takeOver = loop([{ offset: 0, opacity: 1 }, { offset: 0.42, opacity: 1 }, { offset: 0.46, opacity: 0 }, { offset: 1, opacity: 0 }]);
  const giveBack = loop([{ offset: 0, opacity: 0 }, { offset: 0.44, opacity: 0 }, { offset: 0.5, opacity: 1 }, { offset: 1, opacity: 1 }]);
  return (
    <View class="h-[176px] w-[132px] overflow-hidden rounded-[18px] bg-black px-2 pb-2 pt-2.5">
      <View class="h-[18px] flex-row items-center gap-1">
        <View class="size-[12px] rounded-full bg-white/85" />
        <View class="h-[6px] w-[36px] rounded-full bg-white/70" />
        <View class="flex-1" />
        <View class="h-[12px] flex-row items-center rounded-full bg-white/15 px-1.5" style={giveBack}>
          <Text class="text-[7px] font-semibold text-white">github.com</Text>
        </View>
      </View>
      <View class="relative mt-1 flex-1">
        {/* The agent's desktop page, at a third of its size. */}
        <View class="absolute inset-x-0 top-[34px] h-[62px] rounded-[3px] bg-white p-1.5" style={desktop}>
          <View class="h-[3px] w-[30px] rounded-full bg-track" />
          <View class="mt-2 self-center gap-1">
            <View class="h-[3px] w-[24px] rounded-full bg-foreground opacity-80" />
            <View class="h-[5px] w-[40px] rounded-[2px] bg-track" />
            <View class="h-[5px] w-[40px] rounded-[2px] bg-track" />
            <View class="h-[5px] w-[40px] rounded-[2px] bg-add" />
          </View>
        </View>
        {/* The same page, laid out for the phone. */}
        <View class="absolute inset-0 gap-1.5 rounded-[4px] border-2 border-seal bg-white px-2 pt-3" style={phone}>
          <View class="h-[7px] w-[60px] rounded-full bg-foreground opacity-80" />
          <View class="mt-1 h-[14px] rounded-[4px] border border-border" />
          <View class="h-[14px] rounded-[4px] border border-border" />
          <View class="h-[14px] items-center justify-center rounded-[4px] bg-add"><Text class="text-[7px] font-semibold text-white">Sign in</Text></View>
        </View>
      </View>
      <View class="relative mt-1.5 h-[20px] items-center">
        <View class="absolute h-[20px] flex-row items-center gap-1 rounded-full bg-white px-2.5" style={takeOver}>
          <Icon name="hand" size={9} tone="ink" />
          <Text class="text-[8px] font-semibold text-black">Take over</Text>
        </View>
        <View class="absolute h-[20px] flex-row items-center gap-1 rounded-full bg-seal px-2.5" style={giveBack}>
          <Icon name="arrow-forward" size={9} tone="onBlack" />
          <Text class="text-[8px] font-semibold text-white">Give back</Text>
        </View>
        <Press at={0.36} style={{ top: -3 }} />
      </View>
    </View>
  );
}

/** Screen on a phone: a tap lands near a small button, the pointer's handle brings it onto it, a tap on the handle clicks. */
function ScreenPointer() {
  // The pointer appears where the tap landed, then the handle carries it 14 px left onto the button.
  const pointer = loop([
    { offset: 0, opacity: 0, translateX: 0 },
    { offset: 0.2, opacity: 0, translateX: 0 },
    { offset: 0.24, opacity: 1, translateX: 0 },
    { offset: 0.42, opacity: 1, translateX: 0 },
    { offset: 0.62, opacity: 1, translateX: -14 },
    { offset: 0.92, opacity: 1, translateX: -14 },
    { offset: 1, opacity: 0, translateX: -14 },
  ]);
  const resting = loop([{ offset: 0, opacity: 0 }, { offset: 0.2, opacity: 0 }, { offset: 0.24, opacity: 1 }, { offset: 0.44, opacity: 1 }, { offset: 0.48, opacity: 0 }, { offset: 1, opacity: 0 }]);
  const solid = loop([{ offset: 0, opacity: 0 }, { offset: 0.44, opacity: 0 }, { offset: 0.48, opacity: 1 }, { offset: 0.92, opacity: 1 }, { offset: 1, opacity: 0 }]);
  const clicked = loop(between(0.76, 0.94));
  return (
    <View class="h-[176px] w-[132px] overflow-hidden rounded-[18px] bg-black px-2 pb-2 pt-2.5">
      <View class="h-[18px] flex-row items-center gap-1">
        <View class="size-[12px] rounded-full bg-white/85" />
        <View class="h-[6px] w-[36px] rounded-full bg-white/70" />
      </View>
      {/* A whole desktop at a third of its size: a toolbar of small buttons. */}
      <View class="relative mt-6 h-[78px] rounded-[3px] bg-white p-1.5">
        <View class="flex-row gap-[3px]">
          {[0, 1, 2, 3, 4, 5, 6, 7].map((i) => (
            <View class="relative size-[8px] rounded-[1px] border border-border">
              <If when={i === 3}><View class="absolute inset-0 bg-add" style={clicked} /></If>
            </View>
          ))}
        </View>
        <View class="mt-2 h-[4px] w-[70px] rounded-full bg-track" />
        <View class="mt-1.5 h-[4px] w-[56px] rounded-full bg-track" />
        <View class="mt-1.5 h-[4px] w-[64px] rounded-full bg-track" />
        <Press at={0.14} style={{ left: 37, top: -1 }} />
        <View class="absolute" style={{ left: 50, top: 10, ...pointer }}>
          <Icon name="navigate" size={11} tone="ink" />
          {/* The handle: faint after a tap, solid once it placed the pointer. */}
          <View class="absolute size-[14px] rounded-full border border-foreground/40" style={{ left: 8, top: 14, ...resting }} />
          <View class="absolute size-[14px] rounded-full border border-foreground/50 bg-white shadow" style={{ left: 8, top: 14, ...solid }} />
        </View>
        <Press at={0.7} style={{ left: 31, top: 23 }} />
      </View>
    </View>
  );
}

/** A picture in a chat: hold it, Save, and it is in the gallery. */
function PictureSave() {
  const menu = loop([{ offset: 0, opacity: 0, translateY: 6 }, { offset: 0.24, opacity: 0, translateY: 6 }, { offset: 0.32, opacity: 1, translateY: 0 }, { offset: 0.92, opacity: 1, translateY: 0 }, { offset: 1, opacity: 0, translateY: 0 }]);
  const chosen = loop([{ offset: 0, opacity: 0 }, { offset: 0.5, opacity: 0 }, { offset: 0.55, opacity: 1 }, { offset: 0.92, opacity: 1 }, { offset: 1, opacity: 0 }]);
  const saved = loop([{ offset: 0, opacity: 0, translateY: 4 }, { offset: 0.6, opacity: 0, translateY: 4 }, { offset: 0.68, opacity: 1, translateY: 0 }, { offset: 0.92, opacity: 1, translateY: 0 }, { offset: 1, opacity: 0, translateY: 0 }]);
  const Row = (props: { icon: IconName; label: string; chosen?: Record<string, unknown> }) => (
    <View class="relative flex-row items-center gap-2 rounded-[10px] px-2.5 py-1.5">
      <If when={props.chosen}><View class="absolute inset-0 rounded-[10px] bg-accent" style={props.chosen} /></If>
      <Icon name={props.icon} size={13} tone="ink" />
      <Text class="text-[12px] text-foreground">{props.label}</Text>
    </View>
  );
  return (
    <View class="w-[250px] flex-row items-start gap-2">
      <View class="relative">
        {/* The picture: a sun over two hills. */}
        <View class="h-[118px] w-[132px] overflow-hidden rounded-[14px] bg-muted">
          <View class="absolute right-[22px] top-[18px] size-[18px] rounded-full bg-seal" />
          <View class="absolute left-[-30px] top-[66px] size-[100px] rounded-[12px] bg-track" style={{ transform: [{ rotate: "45deg" }] }} />
          <View class="absolute left-[50px] top-[80px] size-[110px] rounded-[12px] bg-add" style={{ transform: [{ rotate: "45deg" }] }} />
        </View>
        <Press at={0.12} style={{ left: 52, top: 46 }} />
        <View class="absolute bottom-2 left-0 right-0 items-center" style={saved}>
          <View class="flex-row items-center gap-1 rounded-full bg-primary px-2.5 py-1">
            <Icon name="check" size={11} tone="canvas" />
            <Text class="text-[11px] font-semibold text-primary-foreground">Saved</Text>
          </View>
        </View>
      </View>
      <Card class="relative mt-5 w-[104px] gap-0.5 border border-border p-1" style={menu}>
        <Row icon="download" label="Save" chosen={chosen} />
        <Row icon="share" label="Share" />
        <Press at={0.46} style={{ left: 30, top: 2 }} />
      </Card>
    </View>
  );
}

/** A Work's ⋯: Rename, Move to project, Merge with. */
function WorkMenu() {
  const menu = loop([{ offset: 0, opacity: 0, translateY: -6 }, { offset: 0.2, opacity: 0, translateY: -6 }, { offset: 0.28, opacity: 1, translateY: 0 }, { offset: 0.92, opacity: 1, translateY: 0 }, { offset: 1, opacity: 0, translateY: 0 }]);
  const chosen = loop([{ offset: 0, opacity: 0 }, { offset: 0.5, opacity: 0 }, { offset: 0.55, opacity: 1 }, { offset: 0.92, opacity: 1 }, { offset: 1, opacity: 0 }]);
  const oldProject = loop([{ offset: 0, opacity: 1 }, { offset: 0.6, opacity: 1 }, { offset: 0.64, opacity: 0 }, { offset: 0.96, opacity: 0 }, { offset: 1, opacity: 1 }]);
  const newProject = loop([{ offset: 0, opacity: 0 }, { offset: 0.62, opacity: 0 }, { offset: 0.68, opacity: 1 }, { offset: 0.96, opacity: 1 }, { offset: 1, opacity: 0 }]);
  const Row = (props: { label: string; chosen?: Record<string, unknown> }) => (
    <View class="relative rounded-[10px] px-2.5 py-1.5">
      <If when={props.chosen}><View class="absolute inset-0 rounded-[10px] bg-accent" style={props.chosen} /></If>
      <Text class="text-[12px] text-foreground">{props.label}</Text>
    </View>
  );
  return (
    <View class="w-[250px] gap-1.5">
      <Card class="flex-row items-center gap-2 border border-border px-3 py-2.5">
        <Dot tone="ok" />
        <View class="flex-1 gap-0.5">
          <Text class="text-[13px] font-semibold text-foreground">Fix the sign-in page</Text>
          <View class="relative h-[14px]">
            <Text class="absolute text-[11px] text-muted-foreground" style={oldProject}>notes</Text>
            <Text class="absolute text-[11px] font-semibold text-foreground" style={newProject}>website</Text>
          </View>
        </View>
        <View class="relative size-[24px] items-center justify-center rounded-full bg-muted">
          <Icon name="more-horizontal" size={14} tone="ink" />
          <Press at={0.1} style={{ left: -1, top: -1 }} />
        </View>
      </Card>
      <Card class="relative w-[150px] self-end gap-0.5 border border-border p-1" style={menu}>
        <Row label="Rename" />
        <Row label="Move to project" chosen={chosen} />
        <Row label="Merge with…" />
        <Press at={0.46} style={{ left: 60, top: 30 }} />
      </Card>
    </View>
  );
}

/** Brain is asked to watch a build; its check runs quietly until the build goes red, then Brain acts. */
function TriggerWatch() {
  const checks = [0.14, 0.26, 0.38].map((at) => loop(appear(at, 0)));
  const red = loop(appear(0.5, 0));
  const reply = loop(appear(0.62));
  return (
    <View class="w-[256px] gap-2">
      <View class="self-end rounded-[14px] bg-accent px-3 py-1.5"><Text class="text-[12px] text-foreground">Tell me if the nightly build fails</Text></View>
      <Card class="gap-2 border border-border px-3 py-2.5">
        <View class="flex-row items-center gap-1.5">
          <Icon name="time" size={13} tone="ink" />
          <Text class="flex-1 text-[12px] font-semibold text-foreground">Nightly build</Text>
          <Text class="text-[11px] text-muted-foreground">every 10 min</Text>
        </View>
        <View class="flex-row items-center gap-1.5">
          <View style={checks[0]}><Dot tone="ok" /></View>
          <View style={checks[1]}><Dot tone="ok" /></View>
          <View style={checks[2]}><Dot tone="ok" /></View>
          <View style={red}><Dot tone="seal" /></View>
          <View class="flex-1" />
          <Text class="text-[11px] text-foreground" style={red}>Failed</Text>
        </View>
      </Card>
      <View class="self-start" style={reply}><Text class="text-[12px] text-foreground">It failed on a test. Fixing it now.</Text></View>
    </View>
  );
}

/** The picture the scenes share: a sun over two hills, `w` × `h`. */
function Picture(props: { w: number; h: number; radius?: number }) {
  const k = props.h / 118;
  return (
    <View class="overflow-hidden bg-muted" style={{ width: props.w, height: props.h, borderRadius: props.radius ?? 14 }}>
      <View class="absolute rounded-full bg-seal" style={{ right: 22 * k, top: 18 * k, width: 18 * k, height: 18 * k }} />
      <View class="absolute bg-track" style={{ left: -30 * k, top: 66 * k, width: 100 * k, height: 100 * k, borderRadius: 12 * k, transform: [{ rotate: "45deg" }] }} />
      <View class="absolute bg-add" style={{ left: 50 * k, top: 80 * k, width: 110 * k, height: 110 * k, borderRadius: 12 * k, transform: [{ rotate: "45deg" }] }} />
    </View>
  );
}

/** Android: another app's share sheet offers Mewla; the picture lands in Brain's composer as a chip, nothing sent. */
function ShareIn() {
  const other = loop([{ offset: 0, opacity: 1 }, { offset: 0.42, opacity: 1 }, { offset: 0.48, opacity: 0 }, { offset: 0.96, opacity: 0 }, { offset: 1, opacity: 1 }]);
  const sheet = loop([{ offset: 0, opacity: 0, translateY: 30 }, { offset: 0.06, opacity: 0, translateY: 30 }, { offset: 0.16, opacity: 1, translateY: 0 }, { offset: 0.42, opacity: 1, translateY: 0 }, { offset: 0.48, opacity: 0, translateY: 0 }, { offset: 1, opacity: 0, translateY: 30 }]);
  const composer = loop([{ offset: 0, opacity: 0, translateY: 10 }, { offset: 0.46, opacity: 0, translateY: 10 }, { offset: 0.54, opacity: 1, translateY: 0 }, { offset: 0.92, opacity: 1, translateY: 0 }, { offset: 1, opacity: 0, translateY: 0 }]);
  const chip = loop(appear(0.6, 6));
  const uploading = loop(between(0.6, 0.74));
  const ready = loop([{ offset: 0, opacity: 0 }, { offset: 0.76, opacity: 0 }, { offset: 0.8, opacity: 1 }, { offset: 0.92, opacity: 1 }, { offset: 1, opacity: 0 }]);
  const Target = (props: { icon: IconName; label: string; mewla?: boolean }) => (
    <View class="items-center gap-1">
      <View class={`size-[34px] items-center justify-center rounded-full ${props.mewla ? "bg-foreground" : "bg-muted"}`}>
        <Icon name={props.icon} size={16} tone={props.mewla ? "canvas" : "soft"} />
      </View>
      <Text class={`text-[9px] ${props.mewla ? "font-semibold text-foreground" : "text-muted-foreground"}`}>{props.label}</Text>
    </View>
  );
  return (
    <View class="relative h-[150px] w-[250px]">
      {/* Another app: the picture open, its share sheet up. */}
      <View class="absolute inset-0 items-center" style={other}>
        <Picture w={170} h={92} />
      </View>
      <Card class="absolute bottom-0 left-0 right-0 gap-2 border border-border px-3 pb-2.5 pt-2" style={sheet}>
        <View class="h-[4px] w-[28px] self-center rounded-full bg-track" />
        <View class="relative flex-row justify-around">
          <Target icon="chat" label="Messages" />
          <Target icon="paw" label="Mewla" mewla />
          <Target icon="folder" label="Files" />
          <Target icon="cloud" label="Drive" />
          <Press at={0.34} style={{ left: 79, top: 4 }} />
        </View>
      </Card>
      {/* Mewla: Brain's composer with the picture as a chip, the person's draft still theirs to send. */}
      <View class="absolute inset-0 justify-end gap-2" style={composer}>
        <View class="self-start"><Text class="text-[12px] text-foreground">Morning! All quiet.</Text></View>
        <View class="gap-2 rounded-[20px] border border-border bg-card p-2">
          <View class="w-[150px] flex-row items-center gap-2 rounded-[12px] bg-muted px-2 py-1.5" style={chip}>
            <Picture w={26} h={26} radius={6} />
            <View class="flex-1 gap-0.5">
              <Text class="text-[11px] font-semibold text-foreground line-clamp-1">sunset.jpg</Text>
              <View class="relative h-[12px]">
                <Text class="absolute text-[9px] text-muted-foreground" style={uploading}>Uploading</Text>
                <Text class="absolute text-[9px] text-muted-foreground" style={ready}>Picture · 2.1 MB</Text>
              </View>
            </View>
            <Icon name="close" size={12} tone="soft" />
          </View>
          <View class="flex-row items-center gap-2 pl-1">
            <Text class="flex-1 text-[12px] text-muted-foreground">What should Brain do?</Text>
            <View class="size-[26px] items-center justify-center rounded-full bg-foreground"><Icon name="arrow-up" size={13} tone="canvas" /></View>
          </View>
        </View>
      </View>
    </View>
  );
}

/** A file's status tile on the Git page (more/git/parts.tsx StatusTile): its mark in its colour on its wash. */
function GitTile(props: { mark: "M" | "A"; tone: "warning" | "add"; alpha: number }) {
  return (
    <View class="size-[20px] items-center justify-center rounded-[6px]" style={{ backgroundColor: withAlpha(color(props.tone), props.alpha) }}>
      <Text class="font-mono text-[10px] font-semibold" style={{ color: color(props.tone) }}>{props.mark}</Text>
    </View>
  );
}

/** A Session's turn ends with files changed: its Git button turns amber; a tap opens the Git page's changes. */
function GitButton() {
  const running = loop([{ offset: 0, opacity: 1 }, { offset: 0.26, opacity: 1 }, { offset: 0.3, opacity: 0 }, { offset: 0.96, opacity: 0 }, { offset: 1, opacity: 1 }]);
  const spin = loop([{ offset: 0, rotate: 0 }, { offset: 0.3, rotate: 360 }, { offset: 1, rotate: 360 }]);
  const edited = loop(appear(0.12, 4));
  const clean = loop([{ offset: 0, opacity: 1 }, { offset: 0.3, opacity: 1 }, { offset: 0.34, opacity: 0 }, { offset: 0.96, opacity: 0 }, { offset: 1, opacity: 1 }]);
  const changed = loop([{ offset: 0, opacity: 0, scale: 1 }, { offset: 0.3, opacity: 0, scale: 1 }, { offset: 0.34, opacity: 1, scale: 1.25 }, { offset: 0.4, opacity: 1, scale: 1 }, { offset: 0.96, opacity: 1, scale: 1 }, { offset: 1, opacity: 0, scale: 1 }]);
  const page = loop([{ offset: 0, opacity: 0, translateY: 60 }, { offset: 0.56, opacity: 0, translateY: 60 }, { offset: 0.66, opacity: 1, translateY: 0 }, { offset: 0.92, opacity: 1, translateY: 0 }, { offset: 1, opacity: 0, translateY: 60 }]);
  return (
    <View class="relative h-[150px] w-[256px] gap-2 overflow-hidden">
      <Card class="flex-row items-center gap-2 border border-border px-3 py-2">
        <Icon name="chevron-left" size={14} tone="ink" />
        <View class="flex-1">
          <Text class="text-[12px] font-semibold text-foreground">website</Text>
          <Text class="text-[10px] text-muted-foreground">Claude</Text>
        </View>
        <View class="relative size-[22px] items-center justify-center">
          <View class="absolute" style={clean}><Icon name="git-branch" size={16} tone="ink" /></View>
          <View class="absolute" style={changed}><Icon name="git-branch" size={16} tone="warning" /></View>
          <Press at={0.48} style={{ left: -2, top: -2 }} />
        </View>
        <Icon name="terminal" size={15} tone="ink" />
        <Icon name="more-horizontal" size={15} tone="ink" />
      </Card>
      <View class="gap-2 px-1">
        <View class="self-end rounded-[14px] bg-accent px-3 py-1.5"><Text class="text-[12px] text-foreground">Fix the login page</Text></View>
        <View class="flex-row items-center gap-1.5" style={edited}>
          <Icon name="edit" size={12} tone="soft" />
          <Text class="text-[11px] text-muted-foreground">Edited login.tsx, added a test</Text>
        </View>
        <View class="flex-row items-center gap-1.5" style={running}>
          <View style={spin}><Icon name="sync" size={11} tone="meter" /></View>
          <Text class="text-[11px] text-muted-foreground">Running</Text>
        </View>
      </View>
      <Card class="absolute bottom-0 left-2 right-2 gap-1.5 rounded-b-none border border-b-0 border-border px-3 pb-3 pt-2" style={page}>
        <View class="h-[4px] w-[28px] self-center rounded-full bg-track" />
        <Text class="text-[12px] font-semibold text-foreground">Changes</Text>
        <View class="flex-row items-center gap-2">
          <GitTile mark="M" tone="warning" alpha={0.12} />
          <Text class="flex-1 font-mono text-[11px] font-semibold text-foreground">login.tsx</Text>
          <Text class="font-mono text-[10px]" style={{ color: color("add") }}>+18</Text>
          <Text class="font-mono text-[10px]" style={{ color: color("del") }}>−6</Text>
        </View>
        <View class="flex-row items-center gap-2">
          <GitTile mark="A" tone="add" alpha={0.13} />
          <Text class="flex-1 font-mono text-[11px] font-semibold text-foreground">login.test.tsx</Text>
          <Text class="font-mono text-[10px]" style={{ color: color("add") }}>+42</Text>
        </View>
      </Card>
    </View>
  );
}

/** Brain has a Worker's result: the Work's ring closes, it reads Done and leaves the list by itself. */
function WorkCloses() {
  const reply = loop(appear(0.22));
  const moving = loop([{ offset: 0, opacity: 1 }, { offset: 0.34, opacity: 1 }, { offset: 0.38, opacity: 0 }, { offset: 0.96, opacity: 0 }, { offset: 1, opacity: 1 }]);
  const closed = loop([{ offset: 0, opacity: 0 }, { offset: 0.34, opacity: 0 }, { offset: 0.38, opacity: 1 }, { offset: 0.96, opacity: 1 }, { offset: 1, opacity: 0 }]);
  const leaves = loop([{ offset: 0, opacity: 1, translateX: 0 }, { offset: 0.58, opacity: 1, translateX: 0 }, { offset: 0.68, opacity: 0, translateX: 30 }, { offset: 0.96, opacity: 0, translateX: 30 }, { offset: 1, opacity: 1, translateX: 0 }]);
  const rises = loop([{ offset: 0, translateY: 0 }, { offset: 0.66, translateY: 0 }, { offset: 0.76, translateY: -46 }, { offset: 0.96, translateY: -46 }, { offset: 1, translateY: 0 }]);
  const two = loop([{ offset: 0, opacity: 1 }, { offset: 0.66, opacity: 1 }, { offset: 0.7, opacity: 0 }, { offset: 0.96, opacity: 0 }, { offset: 1, opacity: 1 }]);
  const one = loop([{ offset: 0, opacity: 0 }, { offset: 0.66, opacity: 0 }, { offset: 0.7, opacity: 1 }, { offset: 0.96, opacity: 1 }, { offset: 1, opacity: 0 }]);
  const Slip = (props: { title: string; percent: number; style?: Record<string, unknown>; done?: { moving: Record<string, unknown>; closed: Record<string, unknown> } }) => (
    <View class="absolute left-0 right-0 h-[46px] flex-row items-center gap-2.5 px-3" style={props.style}>
      <View class="relative size-[32px] items-center justify-center">
        <View class="absolute inset-0" style={props.done?.moving}><ProgressHalo mark={() => ({ kind: "percent", percent: props.percent, tone: "moving" })} /></View>
        <If when={props.done}><View class="absolute inset-0" style={props.done!.closed}><ProgressHalo mark={() => ({ kind: "last" })} /></View></If>
        <View class="size-[24px] items-center justify-center rounded-full bg-muted"><Icon name="terminal" size={12} tone="ink" /></View>
      </View>
      <View class="flex-1 gap-0.5">
        <Text class="text-[12px] font-semibold text-foreground">{props.title}</Text>
        <View class="relative h-[13px]">
          <Text class="absolute text-[10px] text-muted-foreground" style={props.done?.moving}>Running</Text>
          <If when={props.done}><View class="absolute flex-row items-center gap-1" style={props.done!.closed}>
              <Icon name="check" size={10} tone="ok" />
              <Text class="text-[10px] text-muted-foreground">Done</Text>
            </View></If>
        </View>
      </View>
    </View>
  );
  return (
    <View class="w-[256px] gap-2">
      <View class="self-start" style={reply}><Text class="text-[12px] text-foreground">The login fix is live. Closing it.</Text></View>
      <Card class="gap-1 border border-border pt-2">
        <View class="flex-row items-center px-3">
          <Text class="flex-1 text-[12px] font-semibold text-foreground">Work</Text>
          <View class="relative h-[14px] w-[70px] items-end">
            <Text class="absolute text-[10px] text-muted-foreground" style={two}>2 running</Text>
            <Text class="absolute text-[10px] text-muted-foreground" style={one}>1 running</Text>
          </View>
        </View>
        <View class="relative h-[92px] overflow-hidden">
          <Slip title="Fix the login page" percent={70} style={{ top: 0, ...leaves }} done={{ moving, closed }} />
          <View class="absolute left-0 right-0 top-[46px] h-[46px]" style={rises}><Slip title="Write the release notes" percent={30} style={{ top: 0 }} /></View>
        </View>
      </Card>
    </View>
  );
}

/** A Calendar row: the time, the title over one quiet line, how it went at the end. */
function DayRow(props: { time: string; title: string; line?: string; warn?: boolean; end?: "done" | "failed" | "next"; dim?: boolean; style?: Record<string, unknown> }) {
  return (
    <View class="flex-row items-center gap-2 px-3 py-[5px]" style={props.style}>
      <Text class="w-[34px] text-[11px] text-muted-foreground" style={{ opacity: props.dim ? 0.55 : 1 }}>{props.time}</Text>
      <View class="flex-1" style={{ opacity: props.dim ? 0.55 : 1 }}>
        <Text class="text-[12px] font-semibold text-foreground">{props.title}</Text>
        <If when={props.line}>{(line) => <Text class={`text-[10px] ${props.warn ? "text-warning" : "text-muted-foreground"}`}>{line()}</Text>}</If>
      </View>
      <If when={props.end === "done"}><Icon name="check-circle" size={14} tone="ok" /></If>
      <If when={props.end === "failed"}><Icon name="alert-circle" size={14} tone="warning" /></If>
      <If when={props.end === "next"}><Icon name="chevron-right" size={13} tone="faint" /></If>
    </View>
  );
}

/** Calendar: today as a day, what ran and how it went above the now line, what is next below it. */
function CalendarDay() {
  const head = loop(appear(0.02, 4));
  const first = loop(appear(0.1, 6));
  const second = loop(appear(0.22, 6));
  const now = loop(appear(0.36, 0));
  const next = loop(appear(0.46, 6));
  return (
    <View class="w-[256px] gap-2">
      <View class="flex-row items-baseline justify-between px-1" style={head}>
        <Text class="text-[18px] font-bold text-foreground">3 today</Text>
        <Text class="text-[11px] text-muted-foreground">Sunday</Text>
      </View>
      <Card class="overflow-hidden border border-border py-1">
        <DayRow time="8:00" title="Send the weekly report" line="Done · Sent to the team" end="done" dim style={first} />
        <DayRow time="9:30" title="Back up the photos" line="Failed · the disk is full" warn end="failed" style={second} />
        <View class="h-[20px] flex-row items-center gap-2 px-3" style={now}>
          <Text class="w-[34px] text-[11px] font-bold text-foreground">10:44</Text>
          <View class="flex-1 flex-row items-center">
            <View class="rounded-full bg-foreground" style={{ width: 6, height: 6, marginRight: -3 }} />
            <View class="flex-1 bg-foreground" style={{ height: 1.5 }} />
          </View>
        </View>
        <DayRow time="11:30" title="Plan the launch" line="Reminder" end="next" style={next} />
      </Card>
    </View>
  );
}

/** Triggers: one row on Today says how many watch and when one needs you; a tap opens their own page. */
function TriggersPage() {
  const row = loop([{ offset: 0, opacity: 1, translateY: 0 }, { offset: 0.3, opacity: 1, translateY: 0 }, { offset: 0.38, opacity: 0, translateY: -8 }, { offset: 0.96, opacity: 0, translateY: -8 }, { offset: 1, opacity: 1, translateY: 0 }]);
  const page = loop([{ offset: 0, opacity: 0, translateY: 14 }, { offset: 0.34, opacity: 0, translateY: 14 }, { offset: 0.44, opacity: 1, translateY: 0 }, { offset: 0.92, opacity: 1, translateY: 0 }, { offset: 1, opacity: 0, translateY: 0 }]);
  const ask = loop(appear(0.5, 6));
  const rest = loop(appear(0.62, 6));
  const tile = (icon: IconName, seal?: boolean) => (
    <View class="size-[26px] items-center justify-center rounded-[8px] bg-tile">
      <Icon name={icon} size={14} tone="ink" />
      <If when={seal}><View class="absolute -right-0.5 -top-0.5 rounded-full border-2 border-card"><View class="size-[7px] rounded-full bg-seal" /></View></If>
    </View>
  );
  return (
    <View class="relative h-[150px] w-[256px]">
      <View class="absolute inset-x-0 top-0" style={row}>
        <View class="mb-2 px-1"><Text class="text-[18px] font-bold text-foreground">3 today</Text></View>
        <Card class="flex-row items-center gap-2 border border-border px-3 py-2">
          {tile("eye", true)}
          <View class="flex-1">
            <Text class="text-[12px] font-semibold text-foreground">Triggers</Text>
            <Text class="text-[10px] text-muted-foreground">18 watching<Text class="text-seal-ink">{" · 1 needs you"}</Text></Text>
          </View>
          <Icon name="chevron-right" size={13} tone="faint" />
          <Press at={0.24} style={{ right: 6, top: 14 }} />
        </Card>
      </View>
      <View class="absolute inset-x-0 top-0 gap-2" style={page}>
        <View class="px-1"><Text class="text-[18px] font-bold text-foreground">Triggers</Text></View>
        <Card class="gap-1.5 border border-border px-3 py-2" style={ask}>
          <View class="flex-row items-center gap-1.5">
            <View class="size-[7px] rounded-full bg-seal" />
            <Text class="flex-1 text-[12px] font-semibold text-foreground">Nightly build</Text>
            <Text class="text-[10px] text-seal-ink">2 h</Text>
          </View>
          <Text class="text-[10px] text-muted-foreground">Its check keeps failing.</Text>
          <View class="flex-row gap-1.5">
            <View class="rounded-full bg-accent px-2.5 py-1"><Text class="text-[10px] font-semibold text-foreground">Check now</Text></View>
            <View class="rounded-full bg-accent px-2.5 py-1"><Text class="text-[10px] font-semibold text-foreground">Pause</Text></View>
          </View>
        </Card>
        <Card class="overflow-hidden border border-border" style={rest}>
          <View class="flex-row items-center gap-2 px-3 py-1.5">
            {tile("eye")}
            <Text class="flex-1 text-[11px] text-foreground">Inbox zero</Text>
            <Text class="text-[10px] text-muted-foreground">noticed 3 times</Text>
          </View>
        </Card>
      </View>
    </View>
  );
}

/** Mewla for Mac: the cat in the menu bar, a click, and the window with what runs on the left and Brain on the right. */
function MacApp() {
  const window = loop(appear(0.3, 12));
  const reply = loop(appear(0.5, 4));
  return (
    <View class="relative h-[150px] w-[256px] gap-2 overflow-hidden">
      <View class="h-[20px] flex-row items-center gap-2 rounded-[8px] bg-card px-2.5">
        <Line width={28} strong />
        <Line width={22} />
        <Line width={22} />
        <View class="flex-1" />
        <View class="relative size-[16px] items-center justify-center">
          <Icon name="brain" size={13} tone="ink" />
          <Press at={0.16} style={{ left: -5, top: -5 }} />
        </View>
        <Line width={18} />
      </View>
      <Card class="flex-1 flex-row overflow-hidden border border-border" style={window}>
        <View class="w-[86px] gap-1.5 border-r border-border px-2 py-2">
          <View class="flex-row gap-1">
            <View class="size-[6px] rounded-full bg-seal" />
            <View class="size-[6px] rounded-full bg-track" />
            <View class="size-[6px] rounded-full bg-track" />
          </View>
          <View class="flex-row items-center gap-1.5"><Dot tone="seal" /><Line width={52} strong /></View>
          <View class="flex-row items-center gap-1.5"><Dot tone="ok" /><Line width={48} /></View>
          <View class="flex-row items-center gap-1.5"><Dot tone="ok" /><Line width={40} /></View>
          <View class="flex-row items-center gap-1.5"><Dot /><Line width={44} /></View>
        </View>
        <View class="flex-1 gap-2 p-2.5">
          <Text class="text-[11px] font-semibold text-foreground">Brain</Text>
          <View class="self-end rounded-[12px] bg-accent px-2.5 py-1"><Text class="text-[11px] text-foreground">Ship it tonight?</Text></View>
          <View class="gap-1.5" style={reply}>
            <Line width={120} />
            <Line width={96} />
          </View>
        </View>
      </Card>
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
  "chat-diagram": ChatDiagram,
  "screen-reflow": ScreenReflow,
  "screen-pointer": ScreenPointer,
  "picture-save": PictureSave,
  "work-menu": WorkMenu,
  "trigger-watch": TriggerWatch,
  "share-in": ShareIn,
  "git-button": GitButton,
  "work-closes": WorkCloses,
  "calendar-day": CalendarDay,
  "triggers-page": TriggersPage,
  "mac-app": MacApp,
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

/** A scene's own width at most (the widest loop, with its margin), and how much larger a phone shows it. */
const SCENE_WIDTH = 272;
const SCENE_SCALE = 1.16;

/**
 * How large a frame `width` wide shows its scene: a little larger than drawn, so a phone reads it, and smaller on a
 * frame narrower than that (the smallest phone, 320 wide), so the loop is never cut at the frame's sides.
 */
export function sceneScale(width: number): number {
  return width > 0 ? Math.min(SCENE_SCALE, width / SCENE_WIDTH) : SCENE_SCALE;
}

/** A page's picture, in its frame. */
export function Scene(props: { name: string; version: string; target?: string }) {
  const Drawn = SCENES[props.name] ?? Glyph;
  const width = signal(0);
  const scale = () => sceneScale(width());
  return (
    <View testID={`whats-new-scene-${props.name}`} accessibilityElementsHidden class="items-center justify-center overflow-hidden rounded-[22px] bg-sheet-surface"
      style={{ height: Math.round((204 * scale()) / SCENE_SCALE) }} onLayout={({ bounds }) => width.set(bounds.size.width)}>
      {/* Drawn at the size of the app's own parts and shown a little larger, so a phone reads them. */}
      <View style={{ transform: [{ scale: scale() }] }}><Drawn version={props.version} target={props.target} /></View>
    </View>
  );
}
