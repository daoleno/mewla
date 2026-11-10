import { cleanup, colorScheme, keyboardFrame, signal, Text, token, View, type NativeComponentRef } from "nsh";
import { Button } from "nsh/base";
import { TerminalSession } from "../../core/terminal/session";
import type { Transport } from "../../core/transport";
import { Icon, type IconName } from "../icons/Icon";
import { belowSections } from "../shell/Shell";
import { TerminalSurface, type TerminalTheme } from "./contract";

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

/**
 * A Session's terminal: the native view, attached to the Session's tmux pane,
 * with the key bar above the keyboard. Scrolling the view drives tmux's copy
 * mode on the computer; "Latest" comes back to the live screen.
 */
export function TerminalPane(props: { transport: Transport; targetId: string; active: () => boolean }) {
  let surface: NativeComponentRef | undefined;
  const ctrl = signal(false);
  const keyboard = signal(false);
  const scrolledBack = signal(false);
  const problem = signal("");
  const exited = signal<number | null | undefined>(undefined);
  const pending: string[] = [];

  const session = new TerminalSession(props.transport, props.targetId, {
    reset: () => {
      pending.length = 0;
      problem.set("");
      surface ? surface.command("reset", {}) : undefined;
    },
    output: (data) => (surface ? surface.command("write", { data }) : pending.push(data)),
    scrollState: (s) => scrolledBack.set(!s.atBottom),
    exit: (code) => exited.set(code),
    error: (message) => problem.set(message),
  });
  cleanup(() => session.close());

  const type = (data: string) => {
    const out = ctrl() ? withControl(data) : data;
    ctrl.set(false);
    session.input(out);
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

  return (
    <View class="flex-1" style={{ backgroundColor: token("background") }}>
      <TerminalSurface class="flex-1" fontSize={13} theme={theme()}
        ref={(node) => {
          surface = node;
          for (const data of pending.splice(0)) node.command("write", { data });
        }}
        onSize={({ cols, rows }) => session.resize(cols, rows)}
        onInput={({ data }) => type(data)}
        onScroll={({ lines }) => session.scroll(lines)}
        onTapCell={({ col, row }) => {
          keyboard.set(true);
          session.focusPane(col, row);
        }} />
      {problem() || exited() !== undefined ? (
        <View class="absolute left-3 right-3 top-3 rounded-xl border border-border bg-card px-3 py-2">
          <Text testID="terminal-notice" class="text-sm">{exited() !== undefined ? `The program ended${exited() === null ? "" : ` (exit ${exited()})`}.` : problem()}</Text>
        </View>
      ) : null}
      {scrolledBack() ? (
        <Button testID="terminal-latest" onPress={() => session.scrollToBottom()} class="absolute bottom-16 right-3 flex-row items-center gap-1 rounded-full border border-border bg-card px-3 py-1.5 active:bg-accent">
          <Icon name="arrow-down" size={16} />
          <Text class="text-sm">Latest</Text>
        </Button>
      ) : null}
      <View class="flex-row items-center gap-1 border-t border-border bg-background px-1.5 py-1.5"
        style={{ marginBottom: props.active() ? Math.max(0, keyboardFrame().height - belowSections()) : 0 }}>
        <Button testID="terminal-keyboard" onPress={() => {
          const show = keyboardFrame().height === 0;
          surface?.command(show ? "focus" : "blur", {});
          keyboard.set(show);
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
      </View>
    </View>
  );
}
