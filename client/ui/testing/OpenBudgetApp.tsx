// FakeDaemonApp, timing every call that hands frames to the app, for `nsh test` only (chat/OpenBudget.test.tsx).
// The frames of a turn reach the app in one batch, in a call of its own (core/transport.ts): what reads them is
// remade inside it, so that call is what NSH's 2 s budget is about. `app.host.send("app.callsReport")` posts
// `app.calls`: each batch's milliseconds and its frames' types.
import { batch, host } from "nsh";
import "../FakeDaemonApp";
import { setFrameBatcher } from "../../core/transport";

const calls: { ms: number }[] = [];
setFrameBatcher((deliver) => {
  const started = Date.now();
  batch(deliver);
  calls.push({ ms: Date.now() - started });
});
host.on("app.callsReport", () => host.post("app.calls", calls.map((call) => call.ms)));

// Every call, whatever started it (a frame, a timer, a slice of a thread's rows): a beat every 10 ms notes how late it
// ran, which is how long the call before it kept the engine. `app.gapsReport` posts `app.gaps`, the longest first.
const BEAT_MS = 10;
const gaps: number[] = [];
let beat = Date.now();
setInterval(() => {
  const now = Date.now();
  gaps.push(now - beat - BEAT_MS);
  beat = now;
}, BEAT_MS);
host.on("app.gapsReport", () => host.post("app.gaps", [...gaps].sort((a, b) => b - a).slice(0, 10)));
host.on("app.gapsReset", () => {
  gaps.length = 0;
  beat = Date.now();
});
