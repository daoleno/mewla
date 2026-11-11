import { mount, View } from "nsh";
import { createNavStack, NavStack } from "nsh/base";
import { installPlatform } from "../../../core/platform";
import { nshPlatform } from "../../platform";
import { MorePageView } from "../Pages";
import type { MorePage } from "../routes";

/** Resources on a phone that has no computer yet, for `nsh test`: no daemon is ever bound. */
installPlatform(nshPlatform());

const nav = createNavStack<MorePage>([{ name: "resources" }]);

mount(() => (
  <View class="flex-1 bg-background">
    <NavStack state={nav} style={{ flex: 1 }}>{(route) => <MorePageView route={route} nav={nav} onPair={() => {}} />}</NavStack>
  </View>
));
