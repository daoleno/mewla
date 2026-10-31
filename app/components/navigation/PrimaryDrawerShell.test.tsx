import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

// react-native and the shell's leaf modules are mocked: keep that out of the
// shared Bun process.
if (!process.env.MEWLA_DRAWER_SHELL_TEST_CHILD) {
  test("drawer shell keeps its page across a fold, in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_DRAWER_SHELL_TEST_CHILD: "1" },
    });
    if (result.exitCode) {
      throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) =>
    React.createElement(name, props, props.children);
  const passthrough = (props: any) => React.createElement(React.Fragment, null, props.children);
  const dims = { width: 390, height: 844 };

  mock.module("react-native", () => ({
    Keyboard: { dismiss() {} },
    Platform: { OS: "ios" },
    StyleSheet: { create: (value: unknown) => value, hairlineWidth: 1 },
    Text: host("text"),
    View: host("view"),
    useWindowDimensions: () => dims,
  }));
  mock.module("expo-router", () => ({ useIsFocused: () => true }));
  mock.module("react-native-safe-area-context", () => ({
    useSafeAreaInsets: () => ({ top: 0, right: 0, bottom: 0, left: 0 }),
  }));
  mock.module("../../constants/tokens", () => ({
    ContentWidth: { page: 760, reading: 820 },
    Spacing: { lg: 16 },
    TypeScale: { pageTitle: {} },
    useAppColors: () => ({
      bgPrimary: "white",
      borderSubtle: "gray",
      modalBackdrop: "black",
      textPrimary: "black",
    }),
  }));
  mock.module("../workers/SessionsListView", () => ({ SESSIONS_TEXT_INSET: 16 }));
  mock.module("../icons/Icon", () => ({ Icon: host("icon") }));
  mock.module("./PrimaryChromeButton", () => ({ PrimaryChromeButton: host("menuButton") }));
  mock.module("./PrimaryPageAction", () => ({
    PrimaryAppBarPageAction: host("pageAction"),
    PrimaryPageActionProvider: passthrough,
  }));
  mock.module("./PrimarySelectionBar", () => ({
    PrimarySelectionBarProvider: passthrough,
    usePrimarySelectionBarContent: () => null,
  }));
  mock.module("./PrimarySurfaceInteraction", () => ({
    PrimarySurfaceInteractionProvider: passthrough,
  }));
  mock.module("./PrimaryTopSwitch", () => ({ PrimaryTopSwitch: host("topSwitch") }));
  mock.module("./PrimaryDrawerPanel", () => ({
    PrimaryDrawerPanel: (props: any) =>
      React.createElement(props.docked ? "sidebarPanel" : "drawerPanel", props),
  }));
  mock.module("./PrimaryDrawer", () => ({
    PrimaryDrawer: (props: any) =>
      React.createElement("drawerFrame", { drawer: props.drawer }, props.children, props.drawer),
    usePrimaryDrawerController: () => ({ open: false, request() {} }),
  }));
  mock.module("./useDrawerFocusContainment", () => ({ useDrawerFocusContainment() {} }));
  mock.module("./usePrimaryDrawerBack", () => ({ usePrimaryDrawerBack() {} }));
  mock.module("./useDesktopWeb", () => ({ useDesktopWebShellHosted: () => false }));
  mock.module("./useConnectionAttention", () => ({
    useConnectionAttention: () => ({ badge: null, menuLabel: "Menu" }),
  }));
  mock.module("../../services/useAppUpdate", () => ({ useAppUpdatePending: () => false }));

  const { PrimaryDrawerShell } = await import("./PrimaryDrawerShell");

  const events: string[] = [];
  function Page() {
    React.useEffect(() => {
      events.push("mount");
      return () => {
        events.push("unmount");
      };
    }, []);
    return React.createElement("page");
  }
  const render = () => (
    <PrimaryDrawerShell activePrimaryRoute="brain" onSelectPrimaryRoute={() => {}}>
      <Page />
    </PrimaryDrawerShell>
  );
  const resize = (width: number, height: number) => {
    dims.width = width;
    dims.height = height;
  };

  test("folding and unfolding never remounts the page", async () => {
    events.length = 0;
    resize(466, 678);
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(render());
    });
    const page = renderer.root.findByType("page" as any);
    const sidebars = () => renderer.root.findAllByType("sidebarPanel" as any).length;
    const drawerPanels = () => renderer.root.findAllByType("drawerPanel" as any).length;
    expect([sidebars(), drawerPanels()]).toEqual([0, 1]);

    // Unfold: the inner screen has room for a sidebar in both orientations.
    for (const [width, height] of [[951, 669], [669, 951]]) {
      resize(width, height);
      await act(async () => renderer.update(render()));
      expect([sidebars(), drawerPanels()]).toEqual([1, 0]);
      expect(renderer.root.findByType("page" as any)).toBe(page);

      // Fold back, and a phone held sideways, which still has no room.
      for (const [foldedWidth, foldedHeight] of [[466, 678], [932, 430]]) {
        resize(foldedWidth, foldedHeight);
        await act(async () => renderer.update(render()));
        expect([sidebars(), drawerPanels()]).toEqual([0, 1]);
        expect(renderer.root.findByType("page" as any)).toBe(page);
        resize(width, height);
        await act(async () => renderer.update(render()));
      }
    }
    expect(events).toEqual(["mount"]);
  });
}
