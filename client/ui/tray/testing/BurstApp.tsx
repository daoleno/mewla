// The app on a scripted computer whose news comes in bursts, for `nsh test` only. The host hands JS the frames
// that came in together in one call (9 to 36 in a row, measured): `app.host.send("app.burst", { sessions, from })`
// delivers an update of each Session that way, and posts `app.trayRuns` (how often the tray reran for it).
// `app.host.send("app.conversation", { worker, upserts?, activity? })` is a conversation's news, as FakeDaemonApp's;
// `app.host.send("app.trayRunsSince")` posts how often the tray reran since the last post.
import { effect, host } from "nsh";
import { startApp } from "../../App";
import { installPlatform, platform, type SocketLike } from "../../../core/platform";
import { FakeDaemon, type FakeDaemonState } from "../../testing/fakeDaemon";
import { tray } from "../state";

const daemon = new FakeDaemon(((host.params as { daemon?: FakeDaemonState }).daemon ?? {}) as FakeDaemonState);
let socket: SocketLike | null = null;
installPlatform({ ...platform(), openSocket: () => (socket = daemon.open()) });
let runs = 0;
effect(() => {
  tray();
  runs++;
});
host.on<{ sessions: Record<string, unknown>[]; seq: number }>("app.burst", ({ sessions, seq }) => {
  const before = runs;
  sessions.forEach((session, index) => {
    const frame = { type: "worker_session_updated", seq: seq + index, worker_session: { ...session, summary: `Step ${seq + index}` } };
    socket?.onmessage?.({ data: JSON.stringify(frame) });
  });
  const during = runs - before;
  setTimeout(() => host.post("app.trayRuns", { during, after: runs - before }), 50);
});
let mark = 0;
host.on<{ worker: string; upserts?: Record<string, unknown>[]; activity?: Record<string, unknown> | null }>("app.conversation", (update) => {
  mark = runs;
  daemon.conversation(update);
});
host.on("app.trayRunsSince", () => host.post("app.trayRuns", { since: runs - mark }));
startApp();
