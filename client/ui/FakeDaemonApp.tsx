// The app on a scripted computer, for `nsh test` only (never bundled: App.tsx is the app's entry).
// The test gives the computer's state as host params: `host: { params: { daemon: {...} } }`.
import { host } from "nsh";
import "./App";
import { installPlatform, platform } from "../core/platform";
import { FakeDaemon, type FakeDaemonState } from "./testing/fakeDaemon";

const daemon = new FakeDaemon(((host.params as { daemon?: FakeDaemonState }).daemon ?? {}) as FakeDaemonState);
installPlatform({ ...platform(), openSocket: () => daemon.open() });
// A test sends the computer's own news (a Session's update, a file changed there) with `app.host.send("app.daemonPush", frame)`.
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));
// `app.host.send("app.conversation", { worker, upserts?, activity?, snapshot? })`: a conversation's news to its followers.
host.on<{ worker: string; upserts?: Record<string, unknown>[]; activity?: Record<string, unknown> | null; snapshot?: boolean }>("app.conversation", (update) => daemon.conversation(update));
// `app.host.send("app.daemonDrop")`: every connection drops; the app connects again.
host.on("app.daemonDrop", () => daemon.drop());
// `app.host.send("app.daemonDown")`: the computer goes offline and stays so.
host.on("app.daemonDown", () => daemon.goDown());
