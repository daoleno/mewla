import { host, mount, signal, Text, View } from "nsh";
import { NavStack } from "nsh/base";
import { installPlatform } from "../../../core/platform";
import type { BrainSnapshot } from "../../../core/brain";
import type { ConnectionState, Frame } from "../../../core/transport";
import type { StoredServer } from "../../../core/storedServerContract";
import { nshPlatform } from "../../platform";
import { bindDaemon } from "../../more/daemon";
import { MorePageView } from "../../more/Pages";
import { FakeDaemon, testServer } from "../../more/testing/fakeDaemon";
import { brain, connection, current, problem, servers } from "../../session";
import { nav, pageNav } from "../../shell/nav";
import { ComputerPage } from "../ComputerPage";

/**
 * The computer page on the one stack, for `nsh test`: home under it (a stand-in), the computer
 * page on top, its children as the app shows them. The test plays the computer as the More
 * harness does (`app.daemon` answers requests; `app.daemonState`, `app.sessionProblem`), names
 * the other computers (`host.params.servers`) and Brain's state (`app.brain`).
 */
installPlatform(nshPlatform());

const params = host.params as { state?: ConnectionState; server?: Partial<StoredServer>; servers?: StoredServer[] };
const fake = new FakeDaemon({}, testServer(params.server ?? {}), params.state ?? "connected");
fake.request = (async (type: string, body: Record<string, unknown>, options: { reply: string }) => {
  fake.asked.push({ type, body });
  if (fake.state !== "connected") throw new Error("Daemon is not connected.");
  const answer = (await host.call("app.daemon", { type, body })) as Record<string, unknown> | null;
  if (answer && typeof answer.error === "string") throw new Error(answer.error);
  return { type: options.reply, ...(answer ?? {}) };
}) as FakeDaemon["request"];
fake.send = (frame: Frame) => void fake.sent.push(frame);
bindDaemon(fake);
servers.set([fake.server, ...(params.servers ?? [])]);
current.set(fake.server);
connection.set(fake.state);
host.on("app.daemonState", (state) => {
  fake.setState(state as ConnectionState);
  connection.set(state as ConnectionState);
});
host.on("app.sessionProblem", (why) => problem.set(String(why ?? "")));
host.on("app.brain", (snapshot) => brain.set(snapshot as BrainSnapshot));

/** What the page asked its owner to open outside itself. */
const opened = signal("");

nav.setRoutes([{ name: "home" }, { name: "computer" }]);

mount(() => (
  <View class="flex-1 bg-background">
    <NavStack state={nav} style={{ flex: 1 }}>
      {(route) =>
        route.name === "home" ? <View testID="home-stub" class="flex-1" />
          : route.name === "computer" ? <ComputerPage nav={pageNav} onSwitchExecutor={() => opened.set("brain-host")} />
          : route.name === "pair" ? <View testID="pair-stub" class="flex-1" />
          : route.name === "session" || route.name === "allSessions" || route.name === "workDetail" || route.name === "calendar" || route.name === "item" || route.name === "run" || route.name === "workFile"
            ? <View class="flex-1" />
            : <MorePageView route={route} nav={pageNav} onPair={() => nav.push({ name: "pair" })} />}
    </NavStack>
    {opened() ? <Text testID={`opened-${opened()}`}>{opened()}</Text> : null}
  </View>
));
