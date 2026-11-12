import { host, mount, Text, View } from "nsh";
import { NavStack } from "nsh/base";
import { installPlatform } from "../../../core/platform";
import type { BrainSnapshot } from "../../../core/brain";
import type { ConnectionState } from "../../../core/transport";
import type { Worker } from "../../../core/sessions/workers";
import { nshPlatform } from "../../platform";
import { FakeDaemon, testServer } from "../../more/testing/fakeDaemon";
import { bindDaemon } from "../../more/daemon";
import type { StoredServer } from "../../../core/storedServerContract";
import { NavPage } from "../../shell/NavPage";
import { nav } from "../../shell/nav";
import { brain, connection, current, servers, workers } from "../../session";
import { StateControl } from "../StateControl";
import { Tray } from "../Tray";

/**
 * Home's header and tray on the one stack, for `nsh test`: `host.params` sets the computer's
 * Sessions (`workers`), Brain (`brain`) and the connection; `app.workers` / `app.connection`
 * change them; `server` and `computers` set the computer's record and its `computers` answer.
 * A Session page is a plain page titled by its id.
 */
installPlatform(nshPlatform());

const params = host.params as { workers?: Worker[]; brain?: BrainSnapshot; connection?: ConnectionState; server?: Partial<StoredServer>; computers?: Record<string, unknown> };
const server = testServer({ name: "atlas", ...params.server });
// The computer answers `computers` (WIRE §5) when the test gives its message.
bindDaemon(new FakeDaemon(params.computers ? { computers: params.computers } : {}, server, "connected"));
servers.set([server]);
current.set(server);
connection.set(params.connection ?? "connected");
workers.set(params.workers ?? []);
// Brain is running unless the test says otherwise: its host and chat thread are known.
brain.set(params.brain ?? { host_worker: { id: "host", name: "brain", status: "running" }, chat_thread_id: "t1" });
host.on("app.workers", (list) => workers.set(list as unknown as Worker[]));
host.on("app.connection", (state) => connection.set(state as unknown as ConnectionState));

mount(() => (
  <View class="flex-1 bg-background">
    <NavStack state={nav} style={{ flex: 1 }}>
      {(route) =>
        route.name === "home" ? (
          <NavPage title="Brain" headerVisible={false}>
            <View class="flex-1">
              <View class="h-[52px] flex-row items-center px-2"><Text class="text-title font-bold">Brain</Text><StateControl onPairAgain={() => {}} /></View>
              <Tray onPairAgain={() => {}} onNewSession={() => {}} onShowCard={(key) => host.call("app.shownCard", key)} />
            </View>
          </NavPage>
        ) : route.name === "session" ? (
          <NavPage title={`Session ${route.workerId}`}><Text testID="session-page">{`session ${route.workerId}`}</Text></NavPage>
        ) : (
          <NavPage title={route.name}><Text testID="other-page">{route.name}</Text></NavPage>
        )}
    </NavStack>
  </View>
));
