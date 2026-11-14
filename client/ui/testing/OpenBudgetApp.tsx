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
