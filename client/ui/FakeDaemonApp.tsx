// The app on a scripted computer, for `nsh test` only (never bundled: App.tsx is the app's entry).
// The test gives the computer's state as host params: `host: { params: { daemon: {...} } }`.
import { host, signal, Storage } from "nsh";
import type { AppUpdateState } from "../core/update/appUpdate";
import { startApp } from "./App";
import { bindAppUpdate } from "./update/update";
import { installPlatform, platform } from "../core/platform";
import { FakeDaemon, type FakeDaemonState } from "./testing/fakeDaemon";
import { setScrollTraceSink } from "../core/timeline/scrollTrace";

// A chat list's scroll trace reaches the test as the host post `app.scrollTrace` (`app.host.posts()`).
setScrollTraceSink((entry) => host.post("app.scrollTrace", entry));

const daemon = new FakeDaemon(((host.params as { daemon?: FakeDaemonState }).daemon ?? {}) as FakeDaemonState);
// How many connections the app opened, in storage as `fake:sockets` (`app.storage()`).
let opened = 0;
installPlatform({ ...platform(), openSocket: () => {
  void Storage.setItem("fake:sockets", String(++opened));
  return daemon.open();
} });
// `host: { params: { appUpdate: {...} } }`: this app's update stands so (the event `app.appUpdate` moves it); presses post `app.appUpdatePressed`.
const appUpdateParam = (host.params as { appUpdate?: AppUpdateState }).appUpdate;
if (appUpdateParam) {
  const state = signal<AppUpdateState>(appUpdateParam);
  host.on<AppUpdateState>("app.appUpdate", (next) => state.set(next));
  bindAppUpdate({ state, pending: () => "update" in state(), start: () => {}, press: () => host.post("app.appUpdatePressed") });
}
// A test sends the computer's own news (a Session's update, a file changed there) with `app.host.send("app.daemonPush", frame)`.
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));
// `app.host.send("app.conversation", { worker, upserts?, deletes?, activity?, snapshot? })`: a conversation's news to its followers.
host.on<{ worker: string; upserts?: Record<string, unknown>[]; deletes?: string[]; activity?: Record<string, unknown> | null; snapshot?: boolean }>("app.conversation", (update) => daemon.conversation(update));
// `app.host.send("app.daemonDrop")`: every connection drops; the app connects again.
host.on("app.daemonDrop", () => daemon.drop());
// `app.host.send("app.daemonDown")`: the computer goes offline and stays so.
host.on("app.daemonDown", () => daemon.goDown());
// `app.host.send("app.entryAgain")`: the entry's top level runs again, as a hot patch runs it.
host.on("app.entryAgain", () => startApp());
