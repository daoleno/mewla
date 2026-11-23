// The tray's counts and project chips under every width squeeze (client/scripts/shots/tray-squeeze.js, with --entry):
// the busy desk with the scene's own counts, set as `squeeze.set` { needs, fresh, running } and pushed as the computer
// would (fixture/traySqueeze.ts). Never bundled: App.tsx is the app's entry and does not import this.
import "./seed";
import { host } from "nsh";
import "../App";
import { installPlatform, platform } from "../../core/platform";
import { FakeDaemon } from "../testing/fakeDaemon";
import { scenario } from "./scenario";
import { setPaneWidth, squeezeState, type SqueezeCounts } from "./traySqueeze";

setPaneWidth(280);
const daemon = new FakeDaemon({ ...scenario(), admitsInput: { reply: "On it." } });
installPlatform({ ...platform(), openSocket: () => daemon.open() });
host.on<Record<string, unknown>>("app.daemonPush", (frame) => daemon.push(frame as { type: string }));
host.on<SqueezeCounts>("squeeze.set", (counts) => {
  const state = squeezeState(counts);
  daemon.push({ type: "worker_session_list", seq: 100 + counts.needs + counts.running, worker_sessions: state.worker_sessions });
  daemon.push({ type: "brain_snapshot", brain: state.brain });
});
