// The app on the soft-final mock's scenario (fixture/scenario.ts), for the fidelity sheet on a device:
//   nsh dev --server-only --port 8790 ui/fixture/FixtureApp.tsx   (from client/), then the dev start link.
// Never bundled: App.tsx is the app's entry and does not import this.
import "./seed";
import { host } from "nsh";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario } from "./scenario";

const daemon = new FakeDaemon(scenario());
installPlatform({ ...platform(), openSocket: () => daemon.open() });
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));
