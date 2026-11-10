import { effect, signal, untracked, View } from "nsh";
import { failureText } from "../../../core/more/daemon";
import { virtualScreen } from "../../../core/screen/screen";
import { daemon } from "../daemon";
import { Notice, Page, Section, SwitchRow } from "../kit/kit";

/**
 * Linux: whether the agents' browsers get a screen of their own (the computer's
 * `/browser` settings). One state for the current computer, shared by Screen and
 * Settings → Labs → Virtual screen; a switch of computer drops it.
 */
export interface VirtualScreenState {
  /** null until the computer answered. */
  enabled: boolean | null;
  /** This computer has no virtual screen to offer (not Linux, or no Xvnc). */
  unsupported: boolean;
  busy: boolean;
  error: string;
}

const EMPTY: VirtualScreenState = { enabled: null, unsupported: false, busy: false, error: "" };
export const virtualScreenState = signal<VirtualScreenState>(EMPTY);
let owner = "";

export function readVirtualScreen(): void {
  const server = daemon()?.server;
  if (!server) return;
  void virtualScreen(server)
    .then((answer) => daemon()?.server.id === server.id && virtualScreenState.set({ enabled: answer.enabled, unsupported: answer.unsupported, busy: false, error: "" }))
    .catch((failure) => daemon()?.server.id === server.id && virtualScreenState.set({ ...virtualScreenState(), busy: false, error: failureText(failure, "That didn't finish") }));
}

function setVirtualScreen(on: boolean): void {
  const server = daemon()?.server;
  if (!server || virtualScreenState().busy) return;
  virtualScreenState.set({ ...virtualScreenState(), busy: true, error: "" });
  void virtualScreen(server, on)
    .then((answer) => daemon()?.server.id === server.id && virtualScreenState.set({ enabled: answer.enabled, unsupported: answer.unsupported, busy: false, error: "" }))
    .catch((failure) => daemon()?.server.id === server.id && virtualScreenState.set({ ...virtualScreenState(), busy: false, error: failureText(failure, "That didn't finish") }));
}

/**
 * Call in a component: reads the current computer's virtual screen now and again after
 * every switch of computer, for as long as the component lives.
 */
export function followVirtualScreen(): void {
  effect(() => {
    const id = daemon()?.server.id ?? "";
    untracked(() => {
      if (id !== owner) {
        owner = id;
        virtualScreenState.set(EMPTY);
      }
      readVirtualScreen();
    });
  });
}

export function VirtualScreenSwitch() {
  followVirtualScreen();
  const state = () => virtualScreenState();
  return (
    <View>
      <Section>
        <SwitchRow
          testID="virtual-screen"
          title="Virtual screen"
          subtitle={state().busy ? (state().enabled ? "Turning off…" : "Turning on…") : undefined}
          checked={state().enabled === true}
          busy={state().busy}
          disabled={(state().enabled === null && !state().error) || state().unsupported}
          onChange={setVirtualScreen}
        />
      </Section>
      {state().error ? <Notice tone="failed" title={state().error} action="Retry" onAction={readVirtualScreen} /> : null}
    </View>
  );
}

/** Settings → Labs → Virtual screen: the one switch; what it does shows on Screen. */
export function VirtualScreenPage() {
  return (
    <Page title="Virtual screen" testID="virtual-screen-page">
      <VirtualScreenSwitch />
    </Page>
  );
}
