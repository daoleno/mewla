import { host, mount, signal, Text, View } from "nsh";
import { TerminalNavigator } from "../../../core/terminal/navigation";
import { NO_SEARCH } from "../../../core/terminal/text";
import { TerminalJumpControls, TerminalRendererFailure, TerminalSearchBar } from "../TerminalOverlays";

/**
 * The terminal's overlays on their own, for `nsh test` (the native view renders
 * nothing there): the jump controls of a view scrolled up to `top`, the search
 * bar, the renderer failure. They drive the page's TerminalNavigator over the
 * history rows the test gives; what it tells the view shows as text
 * (`harness-out`, one command per line). `host.params`: `text` (the rows),
 * `flags`, `top`, `failure` (a message: the renderer failed).
 */
type Params = { text?: string[]; flags?: number[]; top?: number; failure?: string };
const params = host.params as Params;
const rows = { text: params.text ?? [], flags: params.flags ?? [] };

const out = signal<string[]>([]);
const say = (line: string) => out.set([...out(), line]);
const searching = signal(false);
const search = signal(NO_SEARCH);
const failure = signal<string | null>(params.failure ?? null);

const nav = new TerminalNavigator(() => rows, {
  scrollTo: (row) => say(`scrollTo:${row}`),
  scrollToBottom: () => say("scrollToBottom"),
  mark: (row) => say(`mark:${row}`),
}, (state) => search.set(state));
nav.setGridRows(9);
nav.viewport(params.top ?? rows.text.length, false);

function Harness() {
  return (
    <View class="flex-1 bg-background">
      <Text testID="harness-out">{out().join("\n")}</Text>
      <TerminalJumpControls onSearch={() => searching.set(true)} onPrompt={(direction) => nav.jumpPrompt(direction)}
        onBottom={() => {
          searching.set(false);
          nav.clearSearch();
          say("scrollToBottom");
        }} />
      {searching() ? (
        <TerminalSearchBar state={search} onQuery={(query) => nav.runSearch(query, 0)} onStep={(step) => nav.step(step)}
          onClose={() => {
            searching.set(false);
            nav.clearSearch();
          }} />
      ) : null}
      {failure() !== null ? (
        <TerminalRendererFailure message={failure()!} onRetry={() => {
          say("retry");
          failure.set(null);
        }} />
      ) : null}
    </View>
  );
}

mount(() => <Harness />);
