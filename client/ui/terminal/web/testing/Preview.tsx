import { mount, signal, Text, token, View, type NativeComponentRef } from "nsh";
import { WebTerminalSurface } from "../WebTerminalSurface";

/**
 * The web terminal on its own, with the same capabilities as the app, for
 * `nsh test` and for a look in a browser (`nsh package --target web` of this
 * entry, served with the page's files at `terminal/`). Once the grid has a
 * size it writes a styled screen and a history, scrolls up into it and asks
 * the page what it holds; what comes back shows as text.
 */
const PALETTE = ["#1d1b19", "#e0685a", "#8fbf7f", "#e3b55f", "#7aa2f7", "#c49bd8", "#6fc1c1", "#d8d2c8", "#57514a", "#ff8a7a", "#a8d898", "#f2cb7c", "#9ab6ff", "#d9b2ea", "#8ed8d8", "#f4f0ea"];
const SCREEN = "\u001b[1;32mmewla\u001b[0m:\u001b[34m~/src\u001b[0m$ ls\r\n\u001b[38;5;208mREADME.md\u001b[0m  \u001b[4mhttps://mewla.dev\u001b[0m  中文\r\n$ ";
const HISTORY = Array.from({ length: 40 }, (_, i) => `\u001b[3${i % 8}mhistory row ${i}\u001b[0m`);

const size = signal("");
const failed = signal("");
const viewport = signal("");
const probe = signal("");
const events = signal<string[]>([]);
const say = (line: string) => events.set([...events(), line].slice(-8));

let surface: NativeComponentRef | null = null;
let started = false;
// Asked whether or not a size came: an answer says the page and the way back to the app work.
setTimeout(() => surface?.command("probe", {}), 2000);

function start() {
  if (started || !surface) return;
  started = true;
  surface.command("write", { data: SCREEN });
  surface.command("history", { reset: true, removed: 0, rows: HISTORY, cols: 40, alternate: false });
  setTimeout(() => {
    surface?.command("scrollTo", { row: 10 });
    surface?.command("mark", { row: 12 });
    surface?.command("probe", {});
  }, 300);
}

function Preview() {
  return (
    <View class="flex-1 bg-background">
      <View class="px-3 py-2">
        <Text testID="preview-size">{`size: ${size()}`}</Text>
        <Text testID="preview-failed">{`failed: ${failed()}`}</Text>
        <Text testID="preview-viewport">{`viewport: ${viewport()}`}</Text>
        <Text testID="preview-probe">{`probe: ${probe()}`}</Text>
        <Text testID="preview-events">{`events: ${events().join(" | ")}`}</Text>
      </View>
      <WebTerminalSurface class="flex-1" fontSize={13}
        theme={{ background: token("background"), foreground: token("foreground"), cursor: token("foreground"), selection: "#3D7EFF55", palette: PALETTE }}
        ref={(node) => {
          surface = node;
        }}
        onSize={({ cols, rows }) => {
          size.set(`${cols}x${rows}`);
          start();
        }}
        onFailed={({ message }) => failed.set(message)}
        onViewport={({ top, atBottom }) => viewport.set(`top=${top} atBottom=${atBottom}`)}
        onProbe={(answer) => probe.set(JSON.stringify(answer))}
        onInput={({ data }) => say(`input ${JSON.stringify(data)}`)}
        onScroll={({ lines }) => say(`scroll ${lines}`)}
        onTapCell={({ col, row, text, index }) => say(`tap ${col},${row} ${index} ${text}`)}
        onCopy={({ text }) => say(`copy ${text}`)} />
    </View>
  );
}

mount(() => <Preview />);
