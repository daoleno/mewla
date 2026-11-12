import { cleanup, Clipboard, colorScheme, effect, keyboardFrame, openUrl, platform, signal, Text, token, untracked, View, type NativeComponentRef } from "nsh";
import { Button } from "nsh/base";
import { BackHandler } from "nsh/mobile";
import { TerminalNavigator } from "../../core/terminal/navigation";
import { TerminalSession } from "../../core/terminal/session";
import { HISTORY_FAILED } from "../../core/terminal/history";
import { linkAction, NO_SEARCH } from "../../core/terminal/text";
import type { Transport } from "../../core/transport";
import { Icon, type IconName } from "../icons/Icon";
import { AttachmentTray, createAttachmentUploads } from "../more/files/Pages";
import { belowSections } from "../shell/Shell";
import { TerminalSurface, type TerminalTheme } from "./contract";
import { TerminalJumpControls, TerminalRendererFailure, TerminalSearchBar } from "./TerminalOverlays";
import { keyboardOnlyWeb, WebTerminalSurface } from "./web/WebTerminalSurface";

/** The phones' native view; the web's is xterm.js in a web view, with the same contract. */
const Surface: typeof TerminalSurface = platform.os === "web" ? WebTerminalSurface : TerminalSurface;
/** Text size: the phones draw at 8 (about 83 columns on a phone, so Claude Code and Codex lay out as on a laptop); the web's larger screens at 13. */
const FONT_SIZE = platform.os === "web" ? 13 : 8;

const PALETTE_DARK = ["#1d1b19", "#e0685a", "#8fbf7f", "#e3b55f", "#7aa2f7", "#c49bd8", "#6fc1c1", "#d8d2c8", "#57514a", "#ff8a7a", "#a8d898", "#f2cb7c", "#9ab6ff", "#d9b2ea", "#8ed8d8", "#f4f0ea"];
const PALETTE_LIGHT = ["#161412", "#b3372b", "#3f7a33", "#946112", "#2c55c0", "#80428f", "#21787a", "#d8d2c8", "#57514a", "#d24a3c", "#4f9441", "#b37a1a", "#3d6be0", "#9b56ab", "#2a9396", "#fbfaf7"];

/** The terminal's colors from the app's theme, light or dark like the rest of the app. */
function theme(): TerminalTheme {
  const dark = colorScheme() === "dark";
  return { background: token("background"), foreground: token("foreground"), cursor: token("foreground"), selection: dark ? "#3D7EFF55" : "#2C55C055", palette: dark ? PALETTE_DARK : PALETTE_LIGHT };
}

type Key = { label?: string; icon?: IconName; data?: string; repeat?: boolean; id: string };
const KEYS: Key[] = [
  { id: "esc", label: "Esc", data: "\u001b" },
  { id: "tab", label: "Tab", data: "\t" },
  { id: "ctrl", label: "Ctrl" },
  { id: "ctrl-c", label: "⌃C", data: "\u0003" },
  { id: "ctrl-d", label: "⌃D", data: "\u0004" },
  { id: "ctrl-b", label: "⌃B", data: "\u0002" },
  { id: "left", icon: "chevron-left", data: "\u001b[D", repeat: true },
  { id: "up", icon: "chevron-up", data: "\u001b[A", repeat: true },
  { id: "down", icon: "chevron-down", data: "\u001b[B", repeat: true },
  { id: "right", icon: "chevron-right", data: "\u001b[C", repeat: true },
];

/** Ctrl armed by the bar turns the next typed letter into its control character. */
export function withControl(data: string): string {
  if (data.length !== 1) return data;
  const code = data.toLowerCase().charCodeAt(0);
  if (code >= 97 && code <= 122) return String.fromCharCode(code - 96);
  if ("[\\]^_".includes(data)) return String.fromCharCode(data.charCodeAt(0) - 64);
  if (data === " ") return "\u0000";
  return data;
}

/** One shell word: single quotes, with any quote inside closed, escaped and reopened. */
export function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'"'"'`)}'`;
}

/** A tapped file path, for the Session's file preview: `line` and `column` when the path carried them (`a.ts:12:5`). */
export type TerminalPathPosition = { line?: number; column?: number };

/**
 * A Session's terminal: the native view, attached to the Session's pane, with
 * the key bar above the keyboard. The view scrolls the history the computer
 * keeps; scrolled up, it offers search, the previous and next prompt and the
 * way back to the live screen. A tapped URL opens in the browser and a tapped
 * file path (or file: URL) goes to `onOpenPath`; text a program copies
 * (OSC 52) reaches the clipboard.
 */
export function TerminalPane(props: { transport: Transport; targetId: string; active: () => boolean; onOpenPath?: (path: string, position: TerminalPathPosition) => void }) {
  let surface: NativeComponentRef | undefined;
  const ctrl = signal(false);
  const keyboard = signal(false);
  /** The view's own history viewport is above the live screen. */
  const scrolledBack = signal(false);
  /** tmux copy mode on the computer (`terminal_scroll_state`). */
  const copyMode = signal(false);
  const problem = signal("");
  const exited = signal<number | null | undefined>(undefined);
  const historyNotice = signal("");
  const searching = signal(false);
  const search = signal(NO_SEARCH);
  const rendererFailure = signal<string | null>(null);
  const pending: Array<[string, unknown]> = [];
  /** Commands for the view, held until it exists. */
  const command = (name: string, args: unknown) => (surface ? surface.command(name, args) : pending.push([name, args]));

  const nav = new TerminalNavigator(() => session.history, {
    scrollTo: (row) => command("scrollTo", { row }),
    scrollToBottom: () => command("scrollToBottom", {}),
    mark: (row) => command("mark", { row }),
  }, (state) => search.set(state));

  const session: TerminalSession = new TerminalSession(props.transport, props.targetId, {
    reset: () => {
      for (let i = pending.length - 1; i >= 0; i--) if (pending[i][0] === "write") pending.splice(i, 1);
      problem.set("");
      copyMode.set(false);
      command("reset", {});
    },
    output: (data) => command("write", { data }),
    scrollState: (s) => copyMode.set(!s.atBottom),
    history: (render) => {
      command("history", render);
      nav.historyChanged(render);
    },
    historyNotice: (text) => historyNotice.set(text),
    exit: (code) => exited.set(code),
    error: (message) => problem.set(message),
  });
  cleanup(() => session.close());

  const type = (data: string) => {
    const out = ctrl() ? withControl(data) : data;
    ctrl.set(false);
    session.input(out);
  };
  // Attach: a picked file goes to the computer, then its path is typed at the prompt, quoted for the shell.
  const uploads = createAttachmentUploads();
  cleanup(() => uploads.dispose());
  const attachProblem = signal("");
  effect(() => {
    const ready = uploads.ready();
    if (ready.length === 0) return;
    untracked(() => {
      for (const file of ready) session.input(`${shellQuote(file.path)} `);
      uploads.clear();
    });
  });
  const attach = () => {
    attachProblem.set("");
    void uploads.pickAndUpload().catch((error) => attachProblem.set(error instanceof Error ? error.message : "Could not attach the file."));
  };

  let holdTimer: ReturnType<typeof setTimeout> | undefined;
  let repeatTimer: ReturnType<typeof setInterval> | undefined;
  const press = (key: Key) => {
    if (key.repeat) return; // sent on press down
    if (key.id === "ctrl") return ctrl.set(!ctrl());
    if (key.data) type(key.data);
  };
  // Arrows: one on touch, then repeating while held, like a hardware key.
  const hold = (key: Key) => {
    if (!key.repeat || !key.data) return;
    release();
    type(key.data);
    holdTimer = setTimeout(() => (repeatTimer = setInterval(() => session.input(key.data!), 70)), 350);
  };
  const release = () => {
    clearTimeout(holdTimer);
    clearInterval(repeatTimer);
  };
  cleanup(release);

  /** Back to the live screen: the view's history and tmux copy mode. */
  const toBottom = () => {
    command("scrollToBottom", {});
    if (copyMode()) session.scrollToBottom();
    copyMode.set(false);
  };
  const closeSearch = () => {
    searching.set(false);
    nav.clearSearch();
  };
  const focusKeyboard = () => {
    command("focus", {});
    keyboard.set(true);
  };
  // Leaving the terminal (the chat, another page) lets go of the keyboard; coming back keeps it down until tapped.
  effect(() => {
    if (props.active()) return;
    untracked(() => {
      if (!keyboard()) return;
      command("blur", {});
      keyboard.set(false);
      ctrl.set(false);
    });
  });

  // The keyboard the terminal raised is its own (no field), so Android's Back would leave the page with it
  // up: Back hides it first (R4#4).
  onBackPress(() => {
    if (!props.active() || !keyboard() || keyboardFrame().height <= 0) return false;
    command("blur", {});
    keyboard.set(false);
    ctrl.set(false);
    return true;
  });

  // A tap: a link opens; elsewhere on the live screen, the keyboard and the pane under the finger.
  const tap = (col: number, row: number, text: string, index: number) => {
    const link = index >= 0 ? linkAction(text, index) : null;
    if (link?.kind === "url") return void openUrl(link.url).catch(() => {});
    if (link?.kind === "path") return props.onOpenPath?.(link.path, { ...(link.line !== undefined ? { line: link.line } : {}), ...(link.column !== undefined ? { column: link.column } : {}) });
    if (row < 0) return;
    if (scrolledBack() || copyMode()) toBottom();
    focusKeyboard();
    session.focusPane(col, row);
  };

  return (
    <View class="flex-1" style={{ backgroundColor: token("background") }}>
      {/* The surface and what floats over it, above the key bar (and so above the keyboard). */}
      <View class="flex-1">
        <Surface class="flex-1" fontSize={FONT_SIZE} theme={theme()}
          ref={(node) => {
            surface = node;
            for (const [name, args] of pending.splice(0)) node.command(name, args);
          }}
          onSize={({ cols, rows }) => {
            rendererFailure.set(null);
            nav.setGridRows(rows);
            session.resize(cols, rows);
          }}
          onFailed={({ message }) => rendererFailure.set(message)}
          onInput={({ data }) => {
            if (scrolledBack()) command("scrollToBottom", {});
            type(data);
          }}
          onScroll={({ lines }) => session.scroll(lines)}
          onViewport={({ top, atBottom }) => {
            nav.viewport(top, atBottom);
            scrolledBack.set(!atBottom);
          }}
          onTapCell={({ col, row, text, index }) => tap(col, row, text, index)}
          onCopy={({ text }) => void Clipboard.writeText(text).catch(() => {})} />
        {problem() || exited() !== undefined ? (
          <View class="absolute left-3 right-3 top-3 rounded-xl border border-border bg-card px-3 py-2">
            <Text testID="terminal-notice" class="text-sm">{exited() !== undefined ? `The program ended${exited() === null ? "" : ` (exit ${exited()})`}.` : problem()}</Text>
          </View>
        ) : (scrolledBack() || copyMode()) && historyNotice() && !searching() ? (
          <Button testID="terminal-history-notice" disabled={historyNotice() !== HISTORY_FAILED} onPress={() => session.retryHistory()}
            class="absolute left-3 right-3 top-3 rounded-xl border border-border bg-card px-3 py-2">
            <Text class="text-sm text-muted-foreground">{historyNotice()}</Text>
          </Button>
        ) : null}
        {(scrolledBack() || copyMode()) && rendererFailure() === null ? (
          <TerminalJumpControls onSearch={() => searching.set(true)} onPrompt={(direction) => nav.jumpPrompt(direction)}
            onBottom={() => {
              closeSearch();
              toBottom();
            }} />
        ) : null}
        {searching() && rendererFailure() === null ? (
          <TerminalSearchBar state={search} onQuery={(query) => nav.runSearch(query, 0)} onStep={(step) => nav.step(step)} onClose={closeSearch} />
        ) : null}
        {rendererFailure() !== null ? (
          <TerminalRendererFailure message={rendererFailure()!} onRetry={() => command("retry", {})} />
        ) : null}
      </View>
      {uploads.items().length > 0 ? <AttachmentTray uploads={uploads} /> : null}
      {attachProblem() ? <Text testID="terminal-attach-problem" class="px-3 py-1 text-xs text-destructive">{attachProblem()}</Text> : null}
      <View class="flex-row items-center gap-1 border-t border-border bg-background px-1.5 py-1.5"
        style={{ marginBottom: props.active() ? Math.max(0, keyboardFrame().height - belowSections()) : 0 }}>
        <Button testID="terminal-attach" accessibilityLabel="Attach" disabled={uploads.picking()} onPress={attach} class="rounded-lg px-2 py-2 active:bg-accent">
          <Icon name="attach" size={16} />
        </Button>
        {/* A physical keyboard has every key here (keyboard-only web): only Attach stays. */}
        {keyboardOnlyWeb() ? null : (<>
        <Button testID="terminal-keyboard" onPress={() => {
          const show = keyboardFrame().height === 0;
          if (show) focusKeyboard();
          else {
            command("blur", {});
            keyboard.set(false);
          }
        }} class="rounded-lg px-2 py-2 active:bg-accent">
          <Icon name="keyboard" size={18} color="foreground" />
        </Button>
        {KEYS.map((key) => (
          <Button testID={`key-${key.id}`} onPress={() => press(key)} onPressDown={() => hold(key)} onPressUp={release}
            class={key.id === "ctrl" && ctrl() ? "flex-1 items-center rounded-lg bg-primary py-2" : "flex-1 items-center rounded-lg py-2 active:bg-accent"}>
            {key.icon ? <Icon name={key.icon} size={18} color="foreground" />
              : <Text class={key.id === "ctrl" && ctrl() ? "text-sm font-semibold text-primary-foreground" : "text-sm font-semibold"}>{key.label}</Text>}
          </Button>
        ))}
        </>)}
      </View>
    </View>
  );
}

/** Android's Back while the pane lives; a browser has its own back (NSH's web runtime has no `BackHandler.on`). */
function onBackPress(listener: () => boolean): void {
  if (platform.os === "web") return;
  cleanup(BackHandler.on("press", listener));
}
