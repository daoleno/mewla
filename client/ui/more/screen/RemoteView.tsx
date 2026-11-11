import { platform, Scroll, signal, Text, View, WebView } from "nsh";
import { Input } from "nsh/base";
import type { InputRef, WebViewRef } from "nsh";
import { Button } from "nsh/base";
import { ScreenConnection, type ScreenRoute } from "../../../core/screen/screen";
import { activeModifiers, KEYBOARD_SENTINEL, keyboardEdit, NO_MODIFIERS, spendModifiers, tapModifier, type ModifierState } from "../../../core/screen/screenPageModel";
import { startLiveDeadline, LIVE_DEADLINE_REASON } from "../../../core/screen/screenLiveDeadline";
import { parseScreenViewerMessage, type ScreenHostMessage, type ScreenKey, type ScreenModifier, type ScreenPointerMode } from "../../../core/screen/screenViewerProtocol";
import type { ScreenStreamStats } from "../../../core/screen/screenStream";
import type { StoredServer } from "../../../core/storedServerContract";
import { SCREEN_VIEWER_SOURCE } from "./viewerSource";

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
const KEYS: { key: ScreenKey; label: string; name: string }[] = [
  { key: "Escape", label: "Esc", name: "Escape" },
  { key: "Tab", label: "Tab", name: "Tab" },
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

// The document's base: WebCodecs (the video decoder) exists only in a secure
// context. `.invalid` never resolves and the CSP forbids every request.
const VIEWER_BASE_URL = "https://viewer.mewla.invalid/";

/** Pixel-only document with no network access: the app relays the RFB bytes. */
export function viewerHtml(): string {
  const source = SCREEN_VIEWER_SOURCE.replace(/<\/(script)/gi, "<\\/$1");
  return `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'none'; form-action 'none'; base-uri 'none'">
<style>html,body{margin:0;width:100%;height:100%;background:transparent;overflow:hidden}</style>
</head><body><script type="module">${source}</script></body></html>`;
}

let html: string | null = null;

export type RemotePhase = "connecting" | "live" | "lost";

/** What the viewer page measures of the stream, once a second. */
export interface ViewerStats {
  fps: number;
  kbps: number;
  inputMs: number | null;
  bridgeMs: number | null;
  presentMs: number | null;
}

/**
 * One live view of a remote screen: the socket to the computer, the viewer in
 * its WebView, and what passes between them. RFB bytes from the socket are
 * batched to the viewer once a frame and always ahead of later control; the
 * viewer's bytes go to the socket as they come.
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
  /** The viewer felt a long press (a right-click) or the remote bell rang. */
  onHaptic: (() => void) | null = null;
  private connection: ScreenConnection | null = null;
  private web: WebViewRef | null = null;
  private viewerReady = false;
  private outbox: ScreenHostMessage[] = [];
  private chunks: string[] = [];
  private flushQueued = false;
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
      case "send":
        this.connection?.send(message.data);
        return;
      case "connected":
        this.stopDeadline?.();
        this.stopDeadline = null;
        this.phase.set("live");
        return;
      case "closed":
        this.lose(null);
        return;
      case "stats":
        this.viewerStats.set({ fps: message.fps, kbps: message.kbps, inputMs: message.inputMs, bridgeMs: message.bridgeMs ?? null, presentMs: message.presentMs ?? null });
        return;
      case "haptic":
        this.onHaptic?.();
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
        data: (chunk) => {
          if (attempt !== this.attempt) return;
          this.chunks.push(chunk);
          this.schedule();
        },
        ready: () => {},
        rtt: (ms) => attempt === this.attempt && this.rtt.set(ms),
        locked: (locked) => attempt === this.attempt && this.locked.set(locked),
        streamStats: (stats) => attempt === this.attempt && this.stats.set(stats),
        closed: (reason) => {
          if (attempt !== this.attempt) return;
          if (this.options.onSocketClosed?.(reason)) this.disconnect();
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
    // A hold rides on the socket: closing it lets the agent go on.
    this.holding.set(false);
    this.rtt.set(null);
    this.chunks = [];
    this.outbox = this.outbox.filter((message) => message.type !== "rfb");
    if (connection) this.post([{ type: "close" }]);
  }

  /** An agent display: the agent waits while the person holds it. */
  hold(on: boolean): void {
    this.connection?.control({ type: "hold", on });
    this.holding.set(on);
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

  fit(): void {
    this.post([{ type: "fit" }]);
  }

  key(key: ScreenKey, mods: ScreenModifier[]): void {
    this.post([{ type: "key", key, mods }]);
  }

  text(text: string, mods: ScreenModifier[]): void {
    this.post([{ type: "text", text, mods }]);
  }

  private schedule(): void {
    if (this.flushQueued) return;
    this.flushQueued = true;
    requestAnimationFrame(() => {
      this.flushQueued = false;
      this.flush();
    });
  }

  /** Queued bytes always go ahead of later control messages. */
  private post(messages: ScreenHostMessage[], front = false): void {
    if (this.chunks.length) {
      this.outbox.push({ type: "rfb", data: this.chunks, at: Date.now() });
      this.chunks = [];
    }
    if (front) this.outbox.unshift(...messages);
    else this.outbox.push(...messages);
    this.flush();
  }

  private flush(): void {
    if (this.chunks.length) {
      this.outbox.push({ type: "rfb", data: this.chunks, at: Date.now() });
      this.chunks = [];
    }
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
  let input: InputRef | null = null;
  const soft = needsSoftKeyboard();
  html ??= viewerHtml();

  const typed = (next: string) => {
    const edit = keyboardEdit(next);
    value.set(KEYBOARD_SENTINEL);
    if (edit.kind === "none") return;
    const active = activeModifiers(mods());
    if (edit.kind === "backspace") for (let i = 0; i < edit.count; i++) props.session.key("BackSpace", active);
    else props.session.text(edit.text, active);
    mods.set(spendModifiers(mods()));
  };
  const press = (key: ScreenKey) => {
    props.session.key(key, activeModifiers(mods()));
    mods.set(spendModifiers(mods()));
  };

  return (
    <View class={props.class ?? "flex-1"}>
      <WebView
        ref={(node: WebViewRef) => props.session.attach(node)}
        testID="remote-view"
        accessibilityLabel={props.label}
        class="flex-1 bg-background"
        html={html}
        baseUrl={VIEWER_BASE_URL}
        scrollable={false}
        onMessage={(e) => props.session.onViewer(e.data)}
      />
      {/* A browser's own keyboard types into the picture: no field, no key bar. */}
      {soft ? (
        <Input
          ref={(node: InputRef) => {
            input = node;
          }}
          testID="remote-keyboard"
          accessibilityLabel="Type on the remote screen"
          class="absolute h-px w-px opacity-0"
          value={value()}
          autoCapitalize="none"
          autoCorrect={false}
          autoComplete="off"
          caret={false}
          keyboardType="default"
          onChange={({ value: next }) => typed(next)}
          onSubmit={() => press("Return")}
          onBlur={() => props.onTypingChange(false)}
        />
      ) : null}
      {soft && props.typing ? (
        // A narrow phone may not fit every key: the bar scrolls sideways.
        <Scroll horizontal testID="remote-key-bar" class="grow-0 border-t border-border bg-card" contentClass="flex-row items-center gap-1.5 px-2 py-1.5" keyboardShouldPersistTaps="always">
          {props.modifiers.map((modifier) => {
            const state = () => (mods().locked.includes(modifier.key) ? "locked" : mods().armed.includes(modifier.key) ? "armed" : "off");
            return (
              <Button
                testID={`mod-${modifier.key}`}
                accessibilityLabel={modifier.name}
                selected={state() !== "off"}
                onPress={() => mods.set(tapModifier(mods(), modifier.key))}
                class={`min-w-[40px] items-center rounded-lg px-2 py-1.5 ${state() === "locked" ? "border-2 border-primary bg-secondary" : state() === "armed" ? "border border-primary bg-secondary" : "border border-border"}`}
              >
                <Text class="text-sm font-medium">{modifier.label}</Text>
              </Button>
            );
          })}
          <View class="mx-1 h-5 w-px bg-border" />
          {KEYS.map((key) => (
            <Button testID={`key-${key.key}`} accessibilityLabel={key.name} onPress={() => press(key.key)} class="min-w-[36px] items-center rounded-lg border border-border px-2 py-1.5">
              <Text class="text-sm">{key.label}</Text>
            </Button>
          ))}
        </Scroll>
      ) : null}
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
