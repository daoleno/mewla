import { cleanup, Clipboard, effect, host, keyboardFrame, platform, Scroll, signal, Text, untracked, View, WebView } from "nsh";
import { Input } from "nsh/base";
import type { InputRef, WebViewRef } from "nsh";
import { Button } from "nsh/base";
import { ScreenConnection, type ScreenRoute } from "../../../core/screen/screen";
import { activeModifiers, COMMIT_SETTLE_MS, FIELD_RESET_MS, FieldTyping, fieldNeedsReset, KEYBOARD_SENTINEL, NO_MODIFIERS, spendModifiers, tapModifier, typingKeyboardStep, type ModifierState } from "../../../core/screen/screenPageModel";
import { startLiveDeadline, LIVE_DEADLINE_REASON } from "../../../core/screen/screenLiveDeadline";
import { parseScreenViewerMessage, type ScreenHostMessage, type ScreenKey, type ScreenModifier, type ScreenPointerMode } from "../../../core/screen/screenViewerProtocol";
import type { ScreenStreamStats } from "../../../core/screen/screenStream";
import { keyboardLift } from "../../../core/shell/belowStack";
import { belowSections } from "../../shell/below";
import { echoLine, echoNextChange, fitMessage, NO_REFLOW, pushEcho, shouldFit, type EchoEntry, type StageReflow, type StageSize } from "../../../core/screen/stage";
import type { StoredServer } from "../../../core/storedServerContract";
import { SCREEN_VIEWER_SOURCE } from "./viewerSource";
import { viewerBaseUrl } from "../../viewerBase";

export interface RemoteModifier {
  key: ScreenModifier;
  label: string;
  name: string;
}

export const LINUX_MODIFIERS: RemoteModifier[] = [
  { key: "Control", label: "Ctrl", name: "Control" },
  { key: "Option", label: "Alt", name: "Alt" },
  { key: "Shift", label: "Shift", name: "Shift" },
];
/** Windows' own: Command is the Windows key there. */
export const WINDOWS_MODIFIERS: RemoteModifier[] = [...LINUX_MODIFIERS, { key: "Command", label: "Win", name: "Windows key" }];
export const MAC_MODIFIERS: RemoteModifier[] = [
  { key: "Command", label: "⌘", name: "Command" },
  { key: "Control", label: "⌃", name: "Control" },
  { key: "Option", label: "⌥", name: "Option" },
  { key: "Shift", label: "⇧", name: "Shift" },
];
/** The keys a phone keyboard lacks, in the bar docked above it: editing first, then the arrows. */
const EDIT_KEYS: { key: ScreenKey; label: string; name: string }[] = [
  { key: "BackSpace", label: "⌫", name: "Backspace" },
  { key: "Return", label: "↵", name: "Return" },
  { key: "Tab", label: "Tab", name: "Tab" },
  { key: "Escape", label: "Esc", name: "Escape" },
];
const KEYS: { key: ScreenKey; label: string; name: string }[] = [
  { key: "Left", label: "←", name: "Left arrow" },
  { key: "Up", label: "↑", name: "Up arrow" },
  { key: "Down", label: "↓", name: "Down arrow" },
  { key: "Right", label: "→", name: "Right arrow" },
];

export function modifiersFor(os: string | undefined): RemoteModifier[] {
  return os === "darwin" || os === "macos" ? MAC_MODIFIERS : os === "windows" ? WINDOWS_MODIFIERS : LINUX_MODIFIERS;
}

/**
 * Whether typing goes through the system's soft keyboard (a hidden field, the
 * key bar and a keyboard button). A browser with a mouse has a keyboard of its
 * own: a click gives the picture the keys, and the viewer sends every key,
 * modifiers, shortcuts and paste included, as the Expo web app did.
 */
export function needsSoftKeyboard(): boolean {
  return platform.os !== "web" || !platform.desktop;
}

/** "Full size" is offered where the page host can do it: a browser with a keyboard of its own (parity #29). */
export function canShowFullSize(): boolean {
  return !needsSoftKeyboard() && host.available();
}


/** The viewer document: it plays the computer's video over WebRTC; the app carries its signalling. */
export function viewerHtml(): string {
  const source = SCREEN_VIEWER_SOURCE.replace(/<\/(script)/gi, "<\\/$1");
  return `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'none'; form-action 'none'; base-uri 'none'">
<style>html,body{margin:0;width:100%;height:100%;background:#000;overflow:hidden}</style>
</head><body><script type="module">${source}</script></body></html>`;
}

let html: string | null = null;

export type RemotePhase = "connecting" | "live" | "lost";

/** What the viewer page measures of the stream, once a second. */
export interface ViewerStats {
  fps: number;
  kbps: number;
  inputMs: number | null;
}

/**
 * One live view of a remote screen: the socket to the computer, the viewer in
 * its WebView, and what passes between them: the WebRTC signalling both ways
 * and the control messages. The picture and the input go peer to peer.
 */
export class RemoteSession {
  readonly phase = signal<RemotePhase>("connecting");
  readonly reason = signal<string | null>(null);
  readonly locked = signal(false);
  /** Every pong is a sample, even one as long as the last. */
  readonly rtt = signal<number | null>(null, { equals: false });
  readonly stats = signal<ScreenStreamStats | null>(null);
  readonly viewerStats = signal<ViewerStats | null>(null);
  readonly holding = signal(false);
  /** How an agent display's browser window is shown: laid out for this view while held (reflow sessions). */
  readonly reflow = signal<StageReflow>(NO_REFLOW, { equals: (a, b) => a.on === b.on && a.host === b.host });
  /** The view's size as the viewer measured it. */
  readonly viewSize = signal<StageSize | null>(null);
  /** The size the window was last laid out for, while held. */
  private fitted: StageSize | null = null;
  /** The viewer felt a long press (a right-click). No haptic: the app has two (SPEC §16.3). */
  onHaptic: (() => void) | null = null;
  private connection: ScreenConnection | null = null;
  private web: WebViewRef | null = null;
  private viewerReady = false;
  private outbox: ScreenHostMessage[] = [];
  private stopDeadline: (() => void) | null = null;
  private attempt = 0;
  mode: ScreenPointerMode = "touch";

  constructor(
    private readonly server: StoredServer,
    private readonly options: {
      watch?: boolean;
      worker?: string;
      route?: ScreenRoute;
      align: "top" | "center";
      /** The computer ended the socket; true when the page takes it from here (the watched agent finished) instead of showing it lost. */
      onSocketClosed?: (reason: string | null) => boolean;
      /** An agent display on Screen's stage: holding it lays its browser window out for this view. */
      reflow?: boolean;
    },
  ) {
    this.align = options.align;
  }

  private align: "top" | "center";

  attach(web: WebViewRef): void {
    this.web = web;
  }

  /** Everything the viewer says. */
  onViewer(raw: string): void {
    const message = parseScreenViewerMessage(raw);
    if (!message) return;
    switch (message.type) {
      case "ready":
        this.viewerReady = true;
        this.post([{ type: "mode", mode: this.mode }, { type: "align", align: this.align }], true);
        this.flush();
        return;
      case "signal":
        this.connection?.signal(message.signal);
        return;
      case "connected":
        this.stopDeadline?.();
        this.stopDeadline = null;
        this.phase.set("live");
        return;
      case "stats":
        this.viewerStats.set({ fps: message.fps, kbps: message.kbps, inputMs: message.inputMs });
        return;
      case "haptic":
        this.onHaptic?.();
        return;
      case "size":
        this.viewSize.set({ width: message.width, height: message.height, dpr: message.dpr });
        this.fitIfNeeded();
        return;
      default:
        return;
    }
  }

  connect(): void {
    this.disconnect();
    const attempt = ++this.attempt;
    this.phase.set("connecting");
    this.reason.set(null);
    this.holding.set(false);
    this.rtt.set(null);
    this.stats.set(null);
    this.viewerStats.set(null);
    this.stopDeadline = startLiveDeadline(() => {
      if (attempt === this.attempt) this.lose(LIVE_DEADLINE_REASON);
    });
    const connection = new ScreenConnection(
      this.server,
      {
        open: () => attempt === this.attempt && this.post([{ type: "open" }]),
        signal: (signal) => attempt === this.attempt && this.post([{ type: "signal", signal }]),
        ready: () => {},
        rtt: (ms) => attempt === this.attempt && this.rtt.set(ms),
        locked: (locked) => attempt === this.attempt && this.locked.set(locked),
        reflow: (state) => attempt === this.attempt && this.reflow.set(state),
        streamStats: (stats) => attempt === this.attempt && this.stats.set(stats),
        closed: (reason, outdated) => {
          if (attempt !== this.attempt) return;
          if (!outdated && this.options.onSocketClosed?.(reason)) this.disconnect();
          else this.lose(reason);
        },
      },
      this.options.watch,
      this.options.worker,
      this.options.route,
    );
    this.connection = connection;
    // Signing or opening the socket failed: say why now, not after the deadline.
    connection.connect().catch((error: unknown) => {
      if (attempt === this.attempt) this.lose(error instanceof Error && error.message ? error.message : String(error));
    });
  }

  /** A socket is open or opening. */
  get connected(): boolean {
    return this.connection !== null;
  }

  /** Ends the view and says why (`null`: no reason given, or the person chose to). */
  lose(reason: string | null): void {
    this.disconnect();
    this.reason.set(reason);
    this.phase.set("lost");
  }

  disconnect(): void {
    this.attempt++;
    this.stopDeadline?.();
    this.stopDeadline = null;
    const connection = this.connection;
    this.connection = null;
    connection?.close();
    // A hold rides on the socket: closing it lets the agent go on (and puts its window back).
    this.holding.set(false);
    this.reflow.set(NO_REFLOW);
    this.fitted = null;
    this.rtt.set(null);
    if (connection) this.post([{ type: "close" }]);
  }

  /**
   * An agent display: the agent waits while the person holds it. On the stage, holding also lays the agent's
   * browser window out for this view, and letting go puts it back (the daemon does both).
   */
  hold(on: boolean): void {
    this.connection?.control({ type: "hold", on });
    this.holding.set(on);
    this.fitted = null;
    if (on) this.fitIfNeeded();
    else this.reflow.set(NO_REFLOW);
  }

  /** While held, lays the window out for the view once, and again when the view turns or grows (never for the keyboard). */
  private fitIfNeeded(): void {
    const size = untracked(this.viewSize);
    if (!this.options.reflow || !this.connection || !untracked(this.holding) || !size || !shouldFit(this.fitted, size)) return;
    this.fitted = size;
    this.connection.control(fitMessage(size));
  }

  setMode(mode: ScreenPointerMode): void {
    this.mode = mode;
    this.post([{ type: "mode", mode }]);
  }

  /** A short picture sits at the top, or in the middle (sideways, where it owns the display). */
  setAlign(align: "top" | "center"): void {
    if (align === this.align) return;
    this.align = align;
    this.post([{ type: "align", align }]);
  }

  /** Fits the picture to the view (the view's zoom; the computer doesn't change). */
  fitView(): void {
    this.post([{ type: "fit" }]);
  }

  /**
   * Full size, with the keyboard locked where the browser allows it (⌘W and Ctrl+T reach the remote),
   * as the Expo web app's menu did. The page host does it (`web.fullscreen`): the picture's frame
   * cannot, and the click's user activation is the page's. `label` picks this picture's view.
   */
  fullSize(label: string): Promise<boolean> {
    return host.call<{ keyboardLocked?: boolean } | null>("web.fullscreen", { testID: "remote-view", label })
      .then(() => true, () => false);
  }

  key(key: ScreenKey, mods: ScreenModifier[]): void {
    this.post([{ type: "key", key, mods }]);
  }

  text(text: string, mods: ScreenModifier[]): void {
    this.post([{ type: "text", text, mods }]);
  }

  private post(messages: ScreenHostMessage[], front = false): void {
    if (front) this.outbox.unshift(...messages);
    else this.outbox.push(...messages);
    this.flush();
  }

  private flush(): void {
    if (!this.viewerReady || !this.web || !this.outbox.length) return;
    const batch = this.outbox;
    this.outbox = [];
    this.web.postMessage(JSON.stringify(batch));
  }
}

/**
 * The picture of a remote screen, which is also its input surface (a tap clicks,
 * a drag scrolls, a long press right-clicks, two fingers zoom), and while the
 * keyboard is up a slim bar of the keys a phone keyboard lacks.
 */
export function RemoteView(props: { session: RemoteSession; label: string; modifiers: RemoteModifier[]; typing: boolean; onTypingChange: (on: boolean) => void; class?: string }) {
  const mods = signal<ModifierState>(NO_MODIFIERS);
  const value = signal(KEYBOARD_SENTINEL);
  // What the person typed, shown above the key bar: each character for a second, then a dot (it may be a password).
  const echo = signal<EchoEntry[]>([]);
  const now = signal(Date.now());
  let echoTimer: ReturnType<typeof setTimeout> | null = null;
  const tickEcho = () => {
    if (echoTimer) clearTimeout(echoTimer);
    echoTimer = null;
    const at = Date.now();
    now.set(at);
    const next = echoNextChange(untracked(echo), at);
    if (next !== null) echoTimer = setTimeout(tickEcho, next - at + 10);
  };
  const echoed = (edit: { kind: "text"; text: string } | { kind: "backspace"; count: number }) => {
    echo.set(pushEcho(untracked(echo), edit, Date.now()));
    tickEcho();
  };
  cleanup(() => echoTimer && clearTimeout(echoTimer));
  // Typing ends: the echo goes with it.
  effect(() => {
    if (!props.typing) untracked(() => echo().length && echo.set([]));
  });
  let input: InputRef | null = null;
  const soft = needsSoftKeyboard();
  html ??= viewerHtml();

  // What the field holds as it last reported it; each change is diffed against that, not against the
  // sentinel: a key that lands before a reset reaches the field extends what it held (screen [67] kept every
  // key in the field; resetting on each key sent a fast key twice). The field goes back to the sentinel once
  // typing pauses, or at once when a backspace would have nothing left to delete.
  // An input method's composition (pinyin before its characters are chosen) is held by `field` and only
  // its commit is sent; the field is not put back while one is open.
  const field = new FieldTyping((edit) => {
    const active = activeModifiers(mods());
    for (let i = 0; i < edit.backspace; i++) props.session.key("BackSpace", active);
    if (edit.text) props.session.text(edit.text, active);
    if (!active.length) {
      if (edit.backspace) echoed({ kind: "backspace", count: edit.backspace });
      if (edit.text) echoed({ kind: "text", text: edit.text });
    }
    mods.set(spendModifiers(mods()));
  });
  let resetTimer: ReturnType<typeof setTimeout> | null = null;
  let commitTimer: ReturnType<typeof setTimeout> | null = null;
  const stopReset = () => {
    if (resetTimer) clearTimeout(resetTimer);
    resetTimer = null;
  };
  const resetField = () => {
    stopReset();
    if (field.composing) return;
    field.reset();
    value.set(KEYBOARD_SENTINEL);
  };
  cleanup(() => {
    stopReset();
    if (commitTimer) clearTimeout(commitTimer);
  });
  // The field holds next: the signal follows it (so putting the sentinel back is a change the field
  // sees), and it goes back to the sentinel once typing pauses.
  const settle = (next: string) => {
    value.set(next);
    stopReset();
    if (fieldNeedsReset(next)) queueMicrotask(resetField);
    else resetTimer = setTimeout(resetField, FIELD_RESET_MS);
  };
  const typed = (next: string) => {
    if (field.change(next)) settle(next);
    else stopReset();
  };
  const composed = (on: boolean) => {
    field.composition(on);
    if (on) {
      stopReset();
      return;
    }
    // The commit reaches onChange before the composition ends; a moment's wait also takes one that comes after.
    if (commitTimer) clearTimeout(commitTimer);
    commitTimer = setTimeout(() => {
      commitTimer = null;
      const next = field.flush();
      if (next !== null) settle(next);
    }, COMMIT_SETTLE_MS);
  };
  const press = (key: ScreenKey) => {
    const active = activeModifiers(mods());
    props.session.key(key, active);
    if (key === "BackSpace" && !active.length) echoed({ kind: "backspace", count: 1 });
    if (key === "Return") echo.set([]);
    mods.set(spendModifiers(mods()));
  };
  // The phone's clipboard, typed into the remote field (a password manager's copy, a code from a message).
  const paste = async () => {
    const text = await Clipboard.readText().catch(() => "");
    if (!text) return;
    props.session.text(text, []);
    echo.set(pushEcho(untracked(echo), { kind: "text", text: "\u2022".repeat(Math.min(Array.from(text).length, 24)) }, 0));
    tickEcho();
  };
  // Android's Back hides the keyboard without blurring the field: end typing when it goes down.
  let keyboardSeen = false;
  if (soft) {
    effect(() => {
      const step = typingKeyboardStep(keyboardSeen, props.typing, keyboardFrame().height > 0);
      keyboardSeen = step.seen;
      if (step.end) untracked(() => props.onTypingChange(false));
    });
  }

  // Android draws edge to edge: the keyboard covers the page, so the key bar is lifted above it (as the terminal's is).
  const lift = () => (soft && props.typing ? keyboardLift(keyboardFrame().height, belowSections()) : 0);

  return (
    <View class={props.class ?? "flex-1"} style={{ paddingBottom: lift() }}>
      <WebView
        ref={(node: WebViewRef) => props.session.attach(node)}
        testID="remote-view"
        accessibilityLabel={props.label}
        class="flex-1 bg-black"
        html={html}
        baseUrl={viewerBaseUrl}
        scrollable={false}
        onMessage={(e) => props.session.onViewer(e.data)}
      />
      {/* A browser's own keyboard types into the picture: no field, no key bar. */}
      <If when={soft}><Input
          ref={(node: InputRef) => {
            input = node;
          }}
          testID="remote-keyboard"
          accessibilityLabel="Type on the remote screen"
          class="absolute h-px w-px opacity-0"
          value={value()}
          // The caret stays at the end: what is typed comes after the sentinel, and a backspace always has it to delete.
          selectedRange={{ start: value().length }}
          autoCapitalize="none"
          autoCorrect={false}
          autoComplete="off"
          caret={false}
          keyboardType="default"
          onChange={({ value: next }) => typed(next)}
          onCompositionChange={({ composing }) => composed(composing)}
          onSubmit={() => press("Return")}
          onBlur={() => props.onTypingChange(false)}
        /></If>
      <If when={soft && props.typing && echo().length > 0}><View testID="remote-echo" class="h-7 flex-row items-center justify-end bg-black px-3">
          <Text class="text-sub text-white/80 line-clamp-1" style={{ fontVariant: ["tabular-nums"] }}>{echoLine(echo(), now())}</Text>
        </View></If>
      <If when={soft && props.typing}><Scroll horizontal testID="remote-key-bar" class="grow-0 bg-black" contentClass="flex-row items-center gap-1.5 px-2 py-1.5" keyboardShouldPersistTaps="always">
          <Button testID="key-paste" accessibilityLabel="Paste from this phone" onPress={() => void paste()} class="h-11 min-w-[44px] items-center justify-center rounded-[10px] bg-white px-3 active:opacity-70">
            <Text class="text-sub font-semibold text-black">Paste</Text>
          </Button>
          {EDIT_KEYS.map((key) => (
            <Button testID={`key-${key.key}`} accessibilityLabel={key.name} onPress={() => press(key.key)} class="h-11 min-w-[44px] items-center justify-center rounded-[10px] bg-white/15 px-2.5 active:bg-white/30">
              <Text class="text-sub text-white">{key.label}</Text>
            </Button>
          ))}
          <View class="mx-1 h-5 w-px bg-white/25" />
          {props.modifiers.map((modifier) => {
            const state = () => (mods().locked.includes(modifier.key) ? "locked" : mods().armed.includes(modifier.key) ? "armed" : "off");
            return (
              <Button
                testID={`mod-${modifier.key}`}
                accessibilityLabel={modifier.name}
                selected={state() !== "off"}
                onPress={() => mods.set(tapModifier(mods(), modifier.key))}
                class={`h-11 min-w-[44px] items-center justify-center rounded-[10px] px-2.5 ${state() === "locked" ? "border-2 border-white bg-white/35" : state() === "armed" ? "bg-white/35" : "bg-white/15"}`}
              >
                <Text class="text-sub font-semibold text-white">{modifier.label}</Text>
              </Button>
            );
          })}
          <View class="mx-1 h-5 w-px bg-white/25" />
          {KEYS.map((key) => (
            <Button testID={`key-${key.key}`} accessibilityLabel={key.name} onPress={() => press(key.key)} class="h-11 min-w-[44px] items-center justify-center rounded-[10px] bg-white/15 px-2.5 active:bg-white/30">
              <Text class="text-sub text-white">{key.label}</Text>
            </Button>
          ))}
        </Scroll></If>
      {(() => {
        // Bring the keyboard up or down when the page asks.
        if (!soft) return null;
        if (props.typing) queueMicrotask(() => input?.focus());
        else queueMicrotask(() => input?.blur());
        return null;
      })()}
    </View>
  );
}
