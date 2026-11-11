// The app on a scripted computer, for `nsh test` only (never bundled: App.tsx is the app's entry).
// The test gives the computer's state as host params: `host: { params: { daemon: {...} } }`.
import { host } from "nsh";
import "./App";
import { installPlatform, platform } from "../core/platform";
import { FakeDaemon, type FakeDaemonState } from "./testing/fakeDaemon";

const daemon = new FakeDaemon(((host.params as { daemon?: FakeDaemonState }).daemon ?? {}) as FakeDaemonState);
installPlatform({ ...platform(), openSocket: () => daemon.open() });
// A test sends the computer's own news (a Session's update) with `app.host.send("daemon.push", frame)`.
host.on<Record<string, unknown>>("daemon.push", (frame) => daemon.push(frame as { type: string }));
