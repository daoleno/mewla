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

// How fast this engine runs JS right now, for budgets that hold on a slow or busy runner as on a dev box:
// `app.calibrate` with the events a test sends does fixed text work on them that calls no app code (so a slower app
// cannot raise its own budget): each event's fields copied, its output split into lines, each line trimmed, tested
// and rejoined. It runs `runs` times and posts `app.calibration`, each run's milliseconds.
host.on<{ events: Record<string, unknown>[]; runs?: number }>("app.calibrate", ({ events, runs = 3 }) => {
  const times: number[] = [];
  let kept = 0;
  for (let run = 0; run < runs; run++) {
    const started = Date.now();
    for (const event of events) {
      const copy: Record<string, unknown> = { ...event };
      const lines = String(copy.output ?? "").split("\n").map((line) => line.trim()).filter((line) => /\S/.test(line));
      kept += lines.join("\n").length;
    }
    times.push(Date.now() - started);
  }
  host.post("app.calibration", kept > 0 ? times : []);
});
