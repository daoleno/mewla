import { cleanup, effect, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { failureText } from "../../../core/more/daemon";
import { changeIPhone, getIPhoneStatus, type IPhoneChange, type IPhoneDaemonStatus } from "../../../core/settings/iphoneStatus";
import { daemon } from "../daemon";
import { Row, Section } from "../kit/kit";
import type { PageNav } from "../routes";

/** How often Screen asks the computer whether an agent is on the iPhone. */
const POLL_MS = 5_000;

/**
 * On Screen, while an agent on the current Mac uses the person's iPhone: it works in iPhone
 * Mirroring, off the Mac's screen, so Watch opens iPhone use (its picture), Done tells the agent
 * the person has finished what it asked, Stop stops it. Nothing while no agent is there, or the
 * computer has no iPhone use (an older Mewla, not a Mac).
 */
export function IPhoneRow(props: { nav: PageNav }) {
  const status = signal<IPhoneDaemonStatus | null>(null);
  const busy = signal(false);
  const error = signal("");
  let serverId = "";

  const read = () => {
    const server = daemon()?.server;
    if (!server) return;
    void getIPhoneStatus(server)
      .then((next) => {
        if (daemon()?.server.id === server.id) status.set(next);
      })
      .catch(() => {
        if (daemon()?.server.id === server.id) status.set(null);
      });
  };
  const change = (next: IPhoneChange) => {
    const server = daemon()?.server;
    if (!server || busy()) return;
    busy.set(true);
    error.set("");
    void changeIPhone(server, next)
      .then((answer) => {
        if (daemon()?.server.id === server.id) status.set(answer);
      })
      .catch((failure) => {
        if (daemon()?.server.id === server.id) error.set(failureText(failure, "iPhone use did not answer"));
      })
      .finally(() => busy.set(false));
  };

  // One computer: a switch drops what the last one said.
  effect(() => {
    const id = daemon()?.server.id ?? "";
    untracked(() => {
      if (id === serverId) return;
      serverId = id;
      status.set(null);
      error.set("");
      read();
    });
  });
  const timer = setInterval(read, POLL_MS);
  cleanup(() => clearInterval(timer));

  const agent = () => (status()?.enabled ? status()!.active : undefined);
  const name = () => agent()?.name || "An agent";
  return (
    <View>{agent() ? (
      <View class="w-full max-w-[440px] self-center" testID="screen-iphone">
        <Section>
          <Row icon="phone" title={`${name()} is using your iPhone`} subtitle={error() || undefined} />
          {/* Under the title, not beside it: three buttons beside it leave a phone's width no room for the words. */}
          <View class="flex-row justify-end gap-2 px-4 pb-3">
            <Button testID="screen-iphone-watch" accessibilityLabel={`Watch ${agent()!.name || "the agent"} on your iPhone`} onPress={() => props.nav.push({ name: "settings-iphone" })} class="rounded-full bg-secondary px-3 py-1.5"><Text class="text-sm font-medium">Watch</Text></Button>
            <Button testID="screen-iphone-done" accessibilityLabel="Done: tell the agent you finished on your iPhone" disabled={busy()} onPress={() => change({ handoff_done: true })} class="rounded-full bg-secondary px-3 py-1.5 disabled:opacity-60"><Text class="text-sm font-medium">Done</Text></Button>
            <Button testID="screen-iphone-stop" accessibilityLabel={`Stop ${agent()!.name || "the agent"} on your iPhone`} disabled={busy()} onPress={() => change({ stop_agent: true })} class="rounded-full bg-secondary px-3 py-1.5 disabled:opacity-60"><Text class="text-sm font-medium text-destructive">Stop</Text></Button>
          </View>
        </Section>
      </View>
    ) : null}</View>
  );
}
