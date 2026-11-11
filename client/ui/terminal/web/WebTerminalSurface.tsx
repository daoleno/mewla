import { cleanup, effect, platform, signal, untracked, WebView, type NativeComponentProps, type NativeComponentRef, type WebViewRef } from "nsh";
import { coalesce, parseWebTerminalEvent, terminalPageHtml, WEB_TERMINAL_FONT, webCommand, webTheme, type WebTerminalCommand } from "../../../core/terminal/webSurface";
import type { TerminalEvents, TerminalProps } from "../contract";

/** The contract's events, and `probe` (the `probe` command's answer, for checks). */
export type WebTerminalEvents = TerminalEvents & { probe: { text: string; drawn: string; history: number; grid: string } };

/**
 * Web with no touch pointer: typing comes from a physical keyboard, so the
 * phone's key row stays out (Attach stays). The page says (`any-pointer:
 * coarse`); until it does, the desktop idiom guesses.
 */
export const keyboardOnlyWeb = signal(platform.os === "web" && platform.desktop);

/** The page did not say it runs: its files did not load (a build without them, a development server). */
const READY_TIMEOUT_MS = 15_000;
const NOT_READY = "The terminal page did not load (terminal/terminal.js).";

let html: string | null = null;

/**
 * The terminal view on the web (contract.ts): xterm.js in a web view, the
 * page client/ui/web/terminal/terminal.js. The same props, events and
 * commands as the phones' native view (`probe` besides, for checks), so
 * TerminalPane does not tell them apart. Commands wait for the page; writes
 * go to it once a frame, in order with the rest.
 */
export function WebTerminalSurface(props: NativeComponentProps<TerminalProps, WebTerminalEvents>) {
  let web: WebViewRef | null = null;
  let ready = false;
  let outbox: WebTerminalCommand[] = [];
  let queued = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  html ??= terminalPageHtml();

  const flush = () => {
    if (!ready || !web || outbox.length === 0) return;
    const batch = coalesce(outbox);
    outbox = [];
    web.postMessage(JSON.stringify(batch));
  };
  const post = (message: WebTerminalCommand) => {
    outbox.push(message);
    if (message.type !== "write") return flush();
    if (queued) return;
    queued = true;
    requestAnimationFrame(() => {
      queued = false;
      flush();
    });
  };

  // Props are read in effects: a component runs once.
  let theme: WebTerminalCommand | null = null;
  let font: WebTerminalCommand | null = null;
  effect(() => {
    theme = { type: "theme", theme: webTheme(props.theme) };
    untracked(() => post(theme!));
  });
  effect(() => {
    font = { type: "font", size: Number(props.fontSize) || 13, family: WEB_TERMINAL_FONT };
    untracked(() => post(font!));
  });

  const waitForPage = () => {
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (!ready) props.onFailed?.({ message: NOT_READY });
    }, READY_TIMEOUT_MS);
  };
  cleanup(() => clearTimeout(timer));

  const ref: NativeComponentRef = {
    command(name, args) {
      // A page that never ran loads again; a page whose VT failed makes it again.
      if (name === "retry" && !ready) {
        web?.reload();
        waitForPage();
        return;
      }
      const message = webCommand(name, args);
      if (message) post(message);
    },
    focus: () => post({ type: "focus" }),
    blur: () => post({ type: "blur" }),
    measure: () => web?.measure() ?? Promise.resolve(null),
  };
  if (typeof props.ref === "function") props.ref(ref);

  const onMessage = (raw: string) => {
    const event = parseWebTerminalEvent(raw);
    if (!event) return;
    switch (event.type) {
      case "ready":
        // A page that loads again starts empty: its theme and font first.
        ready = true;
        clearTimeout(timer);
        keyboardOnlyWeb.set(!event.touch);
        outbox = [...[theme, font].filter((m): m is WebTerminalCommand => m !== null), ...outbox];
        flush();
        return;
      case "pointer":
        keyboardOnlyWeb.set(!event.touch);
        return;
      case "size":
        return props.onSize?.({ cols: event.cols, rows: event.rows });
      case "failed":
        return props.onFailed?.({ message: event.message });
      case "input":
        return props.onInput?.({ data: event.data });
      case "scroll":
        return props.onScroll?.({ lines: event.lines });
      case "viewport":
        return props.onViewport?.({ top: event.top, atBottom: event.atBottom });
      case "tapCell":
        return props.onTapCell?.({ col: event.col, row: event.row, text: event.text, index: event.index });
      case "copy":
        return props.onCopy?.({ text: event.text });
      case "probe":
        return props.onProbe?.({ text: event.text, drawn: event.drawn, history: event.history, grid: event.grid });
    }
  };

  return (
    <WebView
      ref={(node: WebViewRef) => {
        web = node;
        flush();
      }}
      testID="terminal-web"
      accessibilityLabel="Terminal"
      class={props.class ?? "flex-1"}
      html={html}
      scrollable={false}
      onLoadStart={() => {
        ready = false;
        waitForPage();
      }}
      onError={(e) => props.onFailed?.({ message: e.message })}
      onMessage={(e) => onMessage(e.data)}
    />
  );
}
