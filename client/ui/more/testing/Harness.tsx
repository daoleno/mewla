import { host, mount, View } from "nsh";
import { createNavStack, NavStack } from "nsh/base";
import { installPlatform } from "../../../core/platform";
import type { ConnectionState, Frame } from "../../../core/transport";
import { nshPlatform } from "../../platform";
import { bindDaemon } from "../daemon";
import { MorePageView } from "../Pages";
import type { MorePage } from "../routes";
import { FakeDaemon, testServer } from "./fakeDaemon";
import { current, servers } from "../../session";

/**
 * A More page on its own, for `nsh test`: the test plays the computer. Its host
 * handler `app.daemon` answers each request (`{type, body}` → the reply's fields, or
 * `{error}`), `app.host.send("app.daemonState", "waiting")` changes the connection and
 * `app.host.send("app.daemonPush", frame)` sends a frame. A request sent as a frame
 * (`codedRequest`) is answered by the same handler; its answer names its `reply` type. `host.params.page` is the route.
 */
installPlatform(nshPlatform());

const params = host.params as { page?: MorePage; below?: MorePage; state?: ConnectionState };
const fake = new FakeDaemon({}, testServer(), params.state ?? "connected");
fake.request = (async (type: string, body: Record<string, unknown>, options: { reply: string }) => {
  fake.asked.push({ type, body });
  if (fake.state !== "connected") throw new Error("Daemon is not connected.");
  const answer = (await host.call("app.daemon", { type, body })) as Record<string, unknown> | null;
  if (answer && typeof answer.error === "string") throw new Error(answer.error);
  return { type: options.reply, ...(answer ?? {}) };
}) as FakeDaemon["request"];
// A request sent as a frame (with a request_id) is answered the same way: the answer's
// `reply` names the frame type that comes back, `{error, code}` comes back as an `error` frame.
function answerSent(daemon: FakeDaemon): FakeDaemon["send"] {
  return (frame: Frame) => {
    daemon.sent.push(frame);
    const { type, request_id: requestId, ...body } = frame;
    if (!requestId) return;
    daemon.asked.push({ type, body });
    void Promise.resolve(host.call("app.daemon", { type, body })).then((raw) => {
      const answer = (raw ?? {}) as Record<string, unknown>;
      if (typeof answer.error === "string") daemon.push({ type: "error", request_id: requestId, message: answer.error, code: answer.code });
      else if (typeof answer.reply === "string") daemon.push({ ...answer, type: answer.reply, request_id: requestId });
    });
  };
}
fake.send = answerSent(fake);
bindDaemon(fake);
// The computers this phone has: the test's one, current.
servers.set([fake.server]);
current.set(fake.server);
host.on("app.daemonState", (state) => fake.setState(state as ConnectionState));
host.on("app.daemonPush", (frame) => fake.push(frame as Frame));
host.on("app.daemonSwitch", (server) => {
  const next = new FakeDaemon({}, testServer(server as object), "connected");
  next.request = fake.request;
  next.send = answerSent(next);
  bindDaemon(next);
});

// `host.params.below`: a page under it, so a test can go back from the page.
const nav = createNavStack<MorePage>([...(params.below ? [params.below] : []), params.page ?? { name: "stats" }]);

mount(() => (
  <View class="flex-1 bg-background">
    <NavStack state={nav} style={{ flex: 1 }}>{(route) => <MorePageView route={route} nav={nav} onPair={() => {}} />}</NavStack>
  </View>
));
