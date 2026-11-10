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
 * `app.host.send("app.daemonPush", frame)` sends a frame. `host.params.page` is the route.
 */
installPlatform(nshPlatform());

const params = host.params as { page?: MorePage; state?: ConnectionState };
const fake = new FakeDaemon({}, testServer(), params.state ?? "connected");
fake.request = (async (type: string, body: Record<string, unknown>, options: { reply: string }) => {
  fake.asked.push({ type, body });
  if (fake.state !== "connected") throw new Error("Daemon is not connected.");
  const answer = (await host.call("app.daemon", { type, body })) as Record<string, unknown> | null;
  if (answer && typeof answer.error === "string") throw new Error(answer.error);
  return { type: options.reply, ...(answer ?? {}) };
}) as FakeDaemon["request"];
bindDaemon(fake);
// The computers this phone has: the test's one, current.
servers.set([fake.server]);
current.set(fake.server);
host.on("app.daemonState", (state) => fake.setState(state as ConnectionState));
host.on("app.daemonPush", (frame) => fake.push(frame as Frame));
host.on("app.daemonSwitch", (server) => {
  const next = new FakeDaemon({}, testServer(server as object), "connected");
  next.request = fake.request;
  bindDaemon(next);
});

const nav = createNavStack<MorePage>([params.page ?? { name: "stats" }]);

mount(() => (
  <View class="flex-1 bg-background">
    <NavStack state={nav} style={{ flex: 1 }}>{(route) => <MorePageView route={route} nav={nav} onPair={() => {}} />}</NavStack>
  </View>
));
