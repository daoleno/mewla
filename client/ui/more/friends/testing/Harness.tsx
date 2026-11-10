import { host, mount, View } from "nsh";
import { createNavStack, NavStack } from "nsh/base";
import { installPlatform } from "../../../../core/platform";
import { nshPlatform } from "../../../platform";
import { current, servers } from "../../../session";
import { bindDaemon } from "../../daemon";
import { restorePet } from "../../pet/pets";
import type { MorePage } from "../../routes";
import { FakeDaemon, testServer } from "../../testing/fakeDaemon";
import { FriendsPageView } from "../FriendsPages";
import { FriendsSettings } from "../FriendsSettings";
import type { FriendsPage } from "../routes";

/**
 * The Friends pages on their own, for `nsh test`: the test's `network` plays the
 * computer's /friends door; `app.host.send("app.daemonSwitch", server)` switches computer.
 */
installPlatform(nshPlatform());
restorePet();

const params = host.params as { page?: MorePage };
const fake = new FakeDaemon({}, testServer(), "connected");
bindDaemon(fake);
servers.set([fake.server]);
current.set(fake.server);
host.on("app.daemonSwitch", (server) => bindDaemon(new FakeDaemon({}, testServer(server as object), "connected")));

const nav = createNavStack<MorePage>([params.page ?? { name: "friends" }]);

mount(() => (
  <View class="flex-1 bg-background">
    <NavStack state={nav} style={{ flex: 1 }}>
      {(route) => (route.name === "settings-friends" ? <FriendsSettings nav={nav} /> : <FriendsPageView route={route as FriendsPage} nav={nav} />)}
    </NavStack>
  </View>
));
