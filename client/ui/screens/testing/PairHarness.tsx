import { host, mount, View } from "nsh";
import { createNavStack, NavStack } from "nsh/base";
import { installPlatform } from "../../../core/platform";
import type { ConnectionState } from "../../../core/transport";
import { nshPlatform } from "../../platform";
import { pairingStatus } from "../../pairing";
import { connection, current, servers } from "../../session";
import { PairPage } from "../Onboarding";

/**
 * Settings → Pair a computer on its own, for `nsh test`, beside a paired computer. The news a phone gets while the
 * page is open: `app.host.send("app.daemonState", state)` changes that computer's connection, `"app.pairingStatus"`
 * what a pairing in flight says.
 */
installPlatform(nshPlatform());

const desk = { id: "s1", name: "Desk", url: "ws://desk.test:9877/ws", daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64), transportKind: "manual" } as never;
servers.set([desk]);
current.set(desk);
connection.set("connected");
host.on("app.daemonState", (state) => connection.set(state as ConnectionState));
host.on("app.pairingStatus", (words) => pairingStatus.set(String(words ?? "")));

const nav = createNavStack<{ name: "pair" }>([{ name: "pair" }]);
mount(() => (
  <View class="flex-1 bg-background">
    <NavStack state={nav} style={{ flex: 1 }}>{() => <PairPage />}</NavStack>
  </View>
));
