import { host, mount, Text, View } from "nsh";
import { createNavStack, NavStack } from "nsh/base";
import { NavPage, PageStack } from "../NavPage";

/**
 * A pushed page on its own, for `nsh test`: its content reports where it lies (`app.footLayout {top, bottom}`),
 * so a test sees whether the page's foot clears the system's bottom bar (`open`'s `safeArea`).
 */
type Route = { name: "under" | "pushed" };
const nav = createNavStack<Route>([{ name: "under" }, { name: "pushed" }]);

mount(() => (
  <View class="flex-1 bg-background">
    <PageStack.Provider value={{ canGoBack: () => nav.routes().length > 1, back: () => nav.pop() }}>
      <NavStack state={nav} style={{ flex: 1 }}>{(route) => (
        <NavPage title="Pushed" testID={`page-${route.name}`}>
          <View testID="foot" class="flex-1" onLayout={({ bounds }) => host.post("app.footLayout", { top: bounds.origin.y, bottom: bounds.origin.y + bounds.size.height })}>
            <Text>{route.name}</Text>
          </View>
        </NavPage>
      )}</NavStack>
    </PageStack.Provider>
  </View>
));
