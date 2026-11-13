import type { JSX } from "nsh";
import { appInfo, Canvas, cleanup, createAnimatedValue, createMotion, effect, reduceMotion, signal, Text, untracked, View, type Accessor } from "nsh";
import { motion, RING } from "../../core/motion/tokens";
import { loops } from "../../core/motion/transitions";
import { Wait, type LinkState, type WaitForce, type WaitState, type WaitView } from "../../core/motion/wait";
import { Icon, type IconName } from "./Icon";
import { color, type ColorName } from "./colors";
import { spinnerRadius } from "../../core/more/spinner";

/**
 * The kit of SPEC §16 every place waits with: one wait per place (`useWait`, the state machine of
 * core/motion/wait.ts on signals), its one cell (`WaitCell`: the placeholder, the content, the named states
 * crossfading in one cell), the slow line, placeholder blocks at the final size, and the delayed ring.
 * The spinner itself is the kit's `Spinner` (kit.tsx).
 */

/** The link to the current computer as the person is told it (session.ts keeps it); every wait follows it. */
export const linkState = signal<LinkState>("connecting");

/** What the kit needs of the current computer, set by session.ts (the kit cannot import it): its name, the link's Try again. */
export const linkHooks = { computer: (): string => "", reconnect: (): void => {} };

/**
 * A state forced in dev builds for the screen in front (slow, no answer, error, offline); null in release
 * builds, where it can never be set. Every mounted wait reads again in it.
 */
export const forcedWait = signal<WaitForce | null>(null);
export const canForceWait = (): boolean => appInfo().development;
/** The link as a forced "offline" plays it for the header and the cat: connecting, then away at `link.away`; null otherwise. */
export const forcedLink = signal<LinkState | null>(null);
let forcedLinkTimer: ReturnType<typeof setTimeout> | undefined;
export function forceWait(force: WaitForce | null): void {
  if (!canForceWait()) return;
  clearTimeout(forcedLinkTimer);
  forcedLink.set(force === "offline" ? "connecting" : null);
  if (force === "offline") forcedLinkTimer = setTimeout(() => forcedLink.set("away"), motion.link.away);
  forcedWait.set(force);
}

export interface WaitHandle {
  view: Accessor<WaitView>;
  slow: Accessor<boolean>;
  dim: Accessor<boolean>;
  reading: Accessor<boolean>;
  /** A read begins; returns its generation. */
  start: () => number;
  resolve: (kind?: "content" | "empty", gen?: number) => void;
  fail: (gen?: number) => void;
  /** Try again (while away: the link's own attempt). */
  retry: () => void;
  /** Last known content is on screen before any read. */
  showKnown: (kind?: "content" | "empty") => void;
}

/**
 * One place's wait. The place starts its own read and calls `start()`; `read` reads again: on Try again,
 * when the link comes back, in a forced state. `reconnect` is the link's Try again while away. Each part of the state is its own signal,
 * so the slow line turning on never touches what reads the view.
 */
export function useWait(options: { read?: () => void; reconnect?: () => void; failMs?: number; known?: boolean } = {}): WaitHandle {
  const view = signal<WaitView>("none");
  const slow = signal(false);
  const dim = signal(false);
  const reading = signal(false);
  const apply = (s: WaitState) => {
    if (untracked(view) !== s.view) view.set(s.view);
    if (untracked(slow) !== s.slow) slow.set(s.slow);
    if (untracked(dim) !== s.dim) dim.set(s.dim);
    if (untracked(reading) !== s.reading) reading.set(s.reading);
  };
  const machine = new Wait({ read: options.read, failMs: options.failMs, link: untracked(linkState), onChange: apply });
  const reconnect = options.reconnect ?? (() => linkHooks.reconnect());
  if (options.known) machine.showKnown();
  effect(() => machine.setLink(linkState()));
  effect(() => machine.setForce(forcedWait()));
  cleanup(() => machine.dispose());
  return {
    view, slow, dim, reading,
    start: () => machine.start(),
    resolve: (kind, gen) => machine.resolve(kind, gen),
    fail: (gen) => machine.fail(gen),
    retry: () => machine.retry(reconnect),
    showKnown: (kind) => machine.showKnown(kind),
  };
}

/** True once `on` has held for `ms` (the ring after `wait.quiet`); false at once when it ends. */
export function delayed(on: Accessor<boolean>, ms: number = motion.wait.quiet): Accessor<boolean> {
  const shown = signal(false);
  effect(() => {
    if (!on()) {
      shown.set(false);
      return;
    }
    const timer = setTimeout(() => shown.set(true), ms);
    return () => clearTimeout(timer);
  });
  return shown;
}

/** Its children only after `wait.quiet`: under it nothing is drawn (SPEC §16.2 pattern 2). */
export function Delayed(props: { children: JSX.Element; ms?: number }) {
  const on = signal(false);
  const timer = setTimeout(() => on.set(true), props.ms ?? motion.wait.quiet);
  cleanup(() => clearTimeout(timer));
  return <If when={on()}>{props.children}</If>;
}

/**
 * The slow line (SPEC §16.2 pattern 6): a 2 pt `soft` line running along the top edge of what waits, from
 * `wait.slow`. With reduced motion it stands still, half strength. It takes no touches and no room.
 */
export function SlowLine(props: { on: boolean; testID?: string }) {
  const width = signal(0);
  const x = createAnimatedValue(0);
  const still = () => !loops("slide", reduceMotion());
  effect(() => {
    const w = width();
    if (!props.on || w <= 0 || still()) {
      void x.stop();
      return;
    }
    const bar = w * 0.34;
    x.set(-bar);
    void x.keyframes([-bar, w], { duration: motion.loop.slide, easing: motion.ease.move as unknown as [number, number, number, number], iterations: "infinite" });
    return () => void x.stop();
  });
  return (
    <View
      testID={`${props.testID ?? "slow-line"}${props.on ? "-on" : ""}`}
      accessibilityElementsHidden
      class="absolute left-0 right-0 top-0 z-10 h-[2px] overflow-hidden"
      style={{ pointerEvents: "none", opacity: props.on ? 1 : 0, transitionProperty: "opacity", transitionDuration: motion.dur.base }}
      onLayout={(e) => width.set(e.bounds.size.width)}
    >
      <If when={still()} else={<View class="absolute bottom-0 top-0 rounded-[1px] bg-muted-foreground" style={{ left: 0, width: width() * 0.34, transform: [{ translateX: x }] }} />}>
        <View class="absolute inset-0 bg-muted-foreground opacity-50" />
      </If>
    </View>
  );
}

/** A placeholder block in `ph` at the final size of what replaces it: no words, no shimmer. */
export function Block(props: { width?: number | `${number}%`; height: number; radius?: number; class?: string }) {
  return <View class={`bg-ph ${props.class ?? ""}`} style={{ width: props.width ?? "100%", height: props.height, borderRadius: props.radius ?? 8 }} />;
}

/** A line of text not there yet: 12 high, radius 6. */
export function Line(props: { width: number | `${number}%`; class?: string }) {
  return <Block width={props.width} height={12} radius={6} class={`my-[6px] ${props.class ?? ""}`} />;
}

const ROW_WIDTHS: Array<[`${number}%`, `${number}%`]> = [["58%", "36%"], ["46%", "42%"], ["64%", "30%"], ["40%", "38%"], ["52%", "34%"]];

/** Rows of a group not there yet, at a row's height (a lead and two lines), in one surface group. */
export function Rows(props: { count: number; lead?: boolean; testID?: string }) {
  return (
    <View testID={props.testID ?? "placeholder-rows"} accessibilityElementsHidden class="overflow-hidden rounded-(--radius-group) bg-card">
      <Each of={Array.from({ length: props.count }, (_, i) => i)} key="index">
        {(i) => (
          <View class="min-h-[64px] flex-row items-center gap-3.5 px-4 py-2">
            <If when={props.lead !== false}><Block width={28} height={28} radius={14} /></If>
            <View class="flex-1">
              <Line width={ROW_WIDTHS[i() % ROW_WIDTHS.length]![0]} />
              <Line width={ROW_WIDTHS[i() % ROW_WIDTHS.length]![1]} />
            </View>
          </View>
        )}
      </Each>
    </View>
  );
}

/** A thread not there yet: sits on the bottom like the thread, bubbles and reply lines. */
export function Thread(props: { testID?: string }) {
  return (
    <View testID={props.testID ?? "placeholder-thread"} accessibilityElementsHidden class="flex-1 justify-end gap-4 px-4 pb-4">
      <Block width="62%" height={72} radius={22} class="self-end" />
      <View>
        <Line width="92%" />
        <Line width="80%" />
        <Line width="48%" />
      </View>
      <Block width="44%" height={48} radius={22} class="self-end" />
      <View>
        <Line width="86%" />
        <Line width="60%" />
      </View>
    </View>
  );
}

/**
 * A named state in the place's own area (SPEC §16.2 pattern 9): an icon in `soft`, the words in ink, one or
 * two actions. Failure is ink, never red.
 */
export function StateView(props: { icon: IconName; title: string; detail?: string; actions?: Array<{ title: string; onPress: () => void; testID?: string }>; testID?: string; /** On the full-screen remote's black: white words (SPEC §15). */ onBlack?: boolean }) {
  return (
    <View testID={props.testID} class="items-center gap-2 px-6 py-12">
      <Icon name={props.icon} size={28} tone={props.onBlack ? "onBlackSoft" : "soft"} />
      <Text class={`mt-1 text-center text-title font-semibold tracking-title ${props.onBlack ? "text-white" : "text-foreground"}`}>{props.title}</Text>
      <If when={props.detail}><Text class={`text-center text-sub ${props.onBlack ? "text-white/60" : "text-muted-foreground"}`}>{props.detail}</Text></If>
      <If when={props.actions?.length}>
        <View class="mt-2 flex-row gap-2">
          <Each of={props.actions}>
            {(a) => (
              <View testID={a().testID} role="button" accessibilityLabel={a().title} onPress={a().onPress} class="h-[44px] items-center justify-center rounded-(--radius-control) bg-card px-4 active:opacity-70">
                <Text class="text-body font-semibold text-foreground">{a().title}</Text>
              </View>
            )}
          </Each>
        </View>
      </If>
    </View>
  );
}

/** The usual named states, worded once. */
export const states = {
  failed: (thing: string, computer: string | undefined, retry: () => void): Parameters<typeof StateView>[0] => ({
    icon: "alert-circle",
    title: `Couldn't read ${thing}`,
    detail: computer ? `${computer} didn't answer` : undefined,
    actions: [{ title: "Try again", onPress: retry, testID: "wait-retry" }],
    testID: "wait-failed",
  }),
  away: (computer: string | undefined, lastSeen: string | undefined, retry: () => void, details?: () => void): Parameters<typeof StateView>[0] => ({
    icon: "cloud-offline",
    title: `${computer ?? "The computer"} is offline`,
    detail: lastSeen ? `Last seen ${lastSeen}` : undefined,
    actions: [{ title: "Try again", onPress: retry, testID: "wait-reconnect" }, ...(details ? [{ title: "Details", onPress: details, testID: "wait-details" }] : [])],
    testID: "wait-away",
  }),
};

/**
 * A layer that fades in when it shows and out before it goes (NSH has no exit on unmount: it plays the
 * fade, then removes it). `float`: drawn over the cell (absolute) instead of in its flow.
 */
function Layer(props: { show: boolean; float: boolean; bottom?: boolean; children: JSX.Element; testID?: string }) {
  const mounted = signal(untracked(() => props.show));
  const leaving = signal(false);
  let exit: (() => Promise<unknown>) | null = null;
  effect(() => {
    if (props.show) {
      leaving.set(false);
      mounted.set(true);
      return;
    }
    if (!untracked(mounted)) return;
    leaving.set(true);
    let live = true;
    void (exit?.() ?? Promise.resolve()).then(() => {
      if (live && !untracked(() => props.show)) {
        mounted.set(false);
        leaving.set(false);
      }
    });
    return () => (live = false);
  });
  const first = untracked(() => props.show);
  return (
    <If when={mounted()}>
      <LayerBody float={props.float || leaving()} bottom={props.bottom} enter={!first} onExit={(f) => (exit = f)} testID={props.testID}>
        {props.children}
      </LayerBody>
    </If>
  );
}

function LayerBody(props: { float: boolean; bottom?: boolean; enter: boolean; onExit: (f: () => Promise<unknown>) => void; children: JSX.Element; testID?: string }) {
  const fade = createMotion(props.enter ? { enter: { keyframes: { opacity: [0, 1] }, duration: motion.dur.base, easing: [...motion.ease.enter] as [number, number, number, number] } } : {});
  props.onExit(() => fade.play({ keyframes: { opacity: [1, 0] }, duration: motion.dur.fast, easing: [...motion.ease.exit] as [number, number, number, number] }));
  return (
    <View testID={props.testID} class={props.float ? `absolute left-0 right-0 ${props.bottom ? "bottom-0" : "top-0"}` : props.bottom ? "flex-1" : ""} style={fade.style}>
      {props.children}
    </View>
  );
}

/**
 * A place's one cell: what it shows while it waits, crossfading in place (SPEC §16.2 patterns 3, 9). The
 * content is made once, the first time it shows, and never again: a forced state or a later wait lays the
 * placeholder over it and fades it, so nothing it holds (a scroll position, a terminal) is re-made. Until
 * content first shows, the current state sits in the cell's flow. `bottom`: a thread's placeholder sits on
 * the bottom, and the swap holds the bottom. The slow line runs along the cell's top.
 */
export function WaitCell(props: {
  wait: WaitHandle;
  placeholder: () => JSX.Element;
  children: JSX.Element;
  failed?: () => JSX.Element;
  away?: () => JSX.Element;
  empty?: () => JSX.Element;
  bottom?: boolean;
  /** The slow line belongs to the page's header instead (the page draws it). */
  noSlowLine?: boolean;
  class?: string;
  testID?: string;
}) {
  const view = props.wait.view;
  const shownOnce = signal(untracked(view) === "content");
  effect(() => {
    if (view() === "content" && !untracked(shownOnce)) shownOnce.set(true);
  });
  const contentVisible = () => view() === "content";
  return (
    <View testID={props.testID ?? "wait-cell"} class={`relative ${props.class ?? ""}`}>
      <If when={shownOnce()}>
        <View
          testID="wait-content"
          class={props.bottom ? "flex-1" : ""}
          style={{ pointerEvents: contentVisible() ? "auto" : "none", opacity: contentVisible() ? (props.wait.dim() ? 0.5 : 1) : 0, transitionProperty: "opacity", transitionDuration: motion.dur.base }}
        >
          {props.children}
        </View>
      </If>
      <Layer show={view() === "placeholder"} float={shownOnce()} bottom={props.bottom} testID="wait-placeholder">
        {props.placeholder()}
      </Layer>
      <If when={props.failed}>
        <Layer show={view() === "failed"} float={shownOnce()} bottom={props.bottom} testID="wait-failed-layer">
          {props.failed!()}
        </Layer>
      </If>
      <If when={props.away}>
        <Layer show={view() === "away"} float={shownOnce()} bottom={props.bottom} testID="wait-away-layer">
          {props.away!()}
        </Layer>
      </If>
      <If when={props.empty}>
        <Layer show={view() === "empty"} float={shownOnce()} bottom={props.bottom} testID="wait-empty-layer">
          {props.empty!()}
        </Layer>
      </If>
      <If when={!props.noSlowLine}>
        <SlowLine on={props.wait.slow()} />
      </If>
    </View>
  );
}

/**
 * The one spinner (SPEC §16.3): an arc in `soft`, one turn per `loop.spin`; 16 pt in a value's place, 20 pt in
 * a pressed button. It turns with reduced motion too (it is information). `label` names what is read
 * ("Reading Model"), never "Loading".
 */
export function Spinner(props: { size?: number; tone?: ColorName; label?: string }) {
  const size = () => props.size ?? RING.button;
  const turn = createMotion({
    enter: { keyframes: { rotate: [0, 360] }, duration: motion.loop.spin, easing: "linear", iterations: "infinite", ignoreReduceMotion: true },
  });
  return (
    <View role="progressbar" accessibilityLabel={props.label ?? "Waiting"} style={{ width: size(), height: size(), ...turn.style }}>
      <Canvas
        style={{ width: size(), height: size() }}
        draw={(cx, s) => {
          const r = spinnerRadius(s.width, s.height);
          if (r === null) return;
          cx.lineWidth = 2;
          cx.lineCap = "round";
          cx.strokeStyle = color(props.tone ?? "soft");
          cx.beginPath();
          cx.arc(s.width / 2, s.height / 2, r, 0, Math.PI * 1.5);
          cx.stroke();
        }}
      />
    </View>
  );
}

/**
 * A button's words while its press waits (SPEC §16.2 pattern 5), for buttons drawn outside the kit: the
 * press alone under `wait.quiet`, then the words fade and the 20 pt ring takes their place; the words keep
 * their room, so the button keeps its size.
 */
export function BusyLabel(props: { busy: boolean; label: string; tone?: ColorName; testID?: string; small?: boolean; children: JSX.Element }) {
  const ring = delayed(() => props.busy);
  return (
    <>
      <View class="flex-row items-center justify-center gap-1.5" style={{ opacity: ring() ? 0 : 1, transitionProperty: "opacity", transitionDuration: motion.dur.fast }}>
        {props.children}
      </View>
      <If when={ring()}>
        <View testID={props.testID ?? "button-ring"} class="absolute inset-0 items-center justify-center" style={{ pointerEvents: "none" }}>
          <Spinner size={props.small ? RING.value : RING.button} label={props.label} tone={props.tone} />
        </View>
      </If>
    </>
  );
}

/**
 * A page part still reading, where the place has no wait of its own (the kit's `EmptyState busy`): nothing
 * under `wait.quiet`, then placeholder rows; the slow line from `wait.slow`; at `wait.fail` on a live link,
 * "{computer} didn't answer" (with Try again where the place gives one); the computer away at `link.away`.
 * What it reads names it for a screen reader (`label`), never on screen.
 */
export function BusyPlace(props: { label?: string; testID?: string; onBlack?: boolean; rows?: number; onRetry?: () => void; failMs?: number }) {
  // A place whose read has a longer honest bound keeps it (SPEC §16.1 `wait.fail`): its own error comes first.
  const wait = useWait({ read: props.onRetry, failMs: props.failMs });
  wait.start();
  const computer = () => linkHooks.computer() || "Your computer";
  return (
    <View testID={props.testID ?? "busy-place"} accessibilityLabel={props.label} class="relative min-h-[120px] pt-4">
      <If when={!props.onBlack}>
        <SlowLine on={wait.slow()} />
      </If>
      <If when={wait.view() === "placeholder" && !props.onBlack}>
        <View class="px-4"><Rows count={props.rows ?? 3} /></View>
      </If>
      <If when={wait.view() === "failed"}>
        <StateView icon="alert-circle" title={`${computer()} didn't answer`} testID="busy-failed" onBlack={props.onBlack}
          actions={props.onRetry ? [{ title: "Try again", onPress: wait.retry, testID: "busy-retry" }] : []} />
      </If>
      <If when={wait.view() === "away"}>
        <StateView {...states.away(linkHooks.computer() || undefined, undefined, wait.retry)} onBlack={props.onBlack} />
      </If>
    </View>
  );
}

export { RING };
