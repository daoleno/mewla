import { host, mount, Text, View } from "nsh";
import { createNavStack, NavPage, NavStack } from "nsh/base";
import { installPlatform } from "../../../core/platform";
import type { StoredServer } from "../../../core/storedServerContract";
import { testServer } from "../../more/testing/fakeDaemon";
import { nshPlatform } from "../../platform";
import { current, ready, servers } from "../../session";
import { PhoneFloat } from "../PhoneFloat";
import { PhonePage } from "../PhonePage";

/**
 * The Phone page and the floating control on their own, for `nsh test` (open it with `platform:
 * "android"`): the test plays the Android host's `phone.*` handlers and sends `phone.status`.
 * `host.params.page` is "phone" (default) or "home", a page the float floats over;
 * `host.params.noServer` starts with no computer. `app.switch {server}` makes another computer
 * current, `app.switch null` leaves none.
 */
installPlatform(nshPlatform());

type Route = "home" | "phone";
const params = host.params as { page?: Route; noServer?: boolean };

function makeCurrent(server: StoredServer | null): void {
  servers.set(server ? [server] : []);
  current.set(server);
}
makeCurrent(params.noServer ? null : testServer());
ready.set(true);
host.on("app.switch", (server) => makeCurrent(server ? testServer(server as Partial<StoredServer>) : null));

const nav = createNavStack<Route>([params.page ?? "phone"]);
const open = () => nav.push("phone");

mount(() => (
  <View class="flex-1 bg-background">
    <NavStack state={nav} style={{ flex: 1 }}>
      {(route) =>
        route === "phone" ? (
          <PhonePage />
        ) : (
          <NavPage title="Home" testID="home-page">
            <View class="flex-1 items-center justify-center">
              <Text>Home</Text>
            </View>
          </NavPage>
        )
      }
    </NavStack>
    <PhoneFloat onOpen={open} />
  </View>
));
