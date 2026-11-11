import { cleanup, effect, Image, signal, Text, untracked, View, lifecycle } from "nsh";
import { changeIPhone, getIPhoneStatus, type IPhoneChange, type IPhoneDaemonStatus } from "../../../core/settings/iphoneStatus";
import { failureText } from "../../../core/more/daemon";
import { daemon } from "../daemon";
import { Notice, Page, Row, Section, SwitchRow } from "../kit/kit";

/**
 * Settings → iPhone use: agents on the current Mac use your iPhone through iPhone
 * Mirroring there. The switch belongs to the computer; the phone does nothing native.
 */
export function IPhone() {
  const status = signal<IPhoneDaemonStatus | null>(null);
  const error = signal("");
  const busy = signal(false);
  /** The picture's height over its width, from the picture itself (an iPhone's, until one loads). */
  const pictureRatio = signal(520 / 240);
  let serverId = "";

  const read = () => {
    const server = daemon()?.server;
    if (!server) return;
    void getIPhoneStatus(server, true)
      .then((next) => {
        if (daemon()?.server.id !== server.id) return;
        status.set(next);
        error.set("");
      })
      .catch((failure) => daemon()?.server.id === server.id && error.set(failureText(failure, "iPhone use did not answer")));
  };
  const change = (next: IPhoneChange) => {
    const server = daemon()?.server;
    if (!server || busy()) return;
    busy.set(true);
    void changeIPhone(server, next)
      .then((answer) => daemon()?.server.id === server.id && status.set(answer))
      .catch((failure) => error.set(failureText(failure, "That didn't finish")))
      .finally(() => busy.set(false));
  };

  // Every two seconds while the page is open: the agent's latest picture of the iPhone.
  let timer: ReturnType<typeof setInterval> | null = null;
  effect(() => {
    const id = daemon()?.server.id ?? "";
    if (timer) clearInterval(timer);
    timer = null;
    const active = lifecycle() === "active";
    untracked(() => {
      if (id !== serverId) {
        serverId = id;
        status.set(null);
        error.set("");
      }
    });
    if (!id || !active) return;
    untracked(read);
    timer = setInterval(read, 2_000);
  });
  cleanup(() => timer && clearInterval(timer));

  const line = () => {
    const s = status();
    if (s) return s.summary;
    return error() ? `This computer doesn't offer iPhone use (${error()}).` : "Reading this computer…";
  };
  const agent = () => status()?.active;
  return (
    <Page title="iPhone use" testID="iphone-page">
      <Section footer={line()}>
        <SwitchRow testID="iphone-switch" title="iPhone use" checked={status()?.enabled === true} busy={busy()} disabled={!status() || status()!.state === "unsupported"} onChange={(on) => change({ enabled: on })} />
      </Section>
      {agent() ? (
        <View>
          {status()?.picture ? (
            <View class="mt-4 items-center">
              <Image
                src={`data:image/jpeg;base64,${status()!.picture}`}
                style={{ width: 240, height: Math.round(240 * pictureRatio()) }}
                class="rounded-2xl"
                accessibilityLabel={`Your iPhone as ${agent()!.name || "the agent"} last saw it`}
                onLoad={({ size }) => size.width > 0 && pictureRatio.set(size.height / size.width)}
              />
            </View>
          ) : null}
          <Section>
            <Row
              title={`${agent()!.name || agent()!.worker} is using your iPhone`}
              trailing={
                <View class="flex-row gap-2">
                  <View role="button" accessibilityLabel="Done: tell the agent you finished" disabled={busy()} accessibilityState={{ disabled: busy() }} onPress={() => change({ handoff_done: true })} class="rounded-full bg-secondary px-3 py-1.5 disabled:opacity-50"><Text class="text-sm font-medium">Done</Text></View>
                  <View role="button" accessibilityLabel={`Stop ${agent()!.name || agent()!.worker}`} disabled={busy()} accessibilityState={{ disabled: busy() }} onPress={() => change({ stop_agent: true })} class="rounded-full bg-secondary px-3 py-1.5 disabled:opacity-50"><Text class="text-sm font-medium text-destructive">Stop</Text></View>
                </View>
              }
            />
          </Section>
        </View>
      ) : null}
      {error() && status() ? <Notice tone="failed" title={error()} /> : null}
    </Page>
  );
}
