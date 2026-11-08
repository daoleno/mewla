import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.MEWLA_BRAIN_WORK_OVERLAY_CHILD) {
  test("the Work sheet is mounted and drawn while shut, so opening commits nothing, in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_BRAIN_WORK_OVERLAY_CHILD: "1" },
    });
    if (result.exitCode) {
      throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) => React.createElement(name, props, props.children);
  const none = () => null;
  const backHandlers: Array<() => boolean> = [];
  const dismissed: string[] = [];

  mock.module("react-native", () => ({
    AccessibilityInfo: { setAccessibilityFocus: () => {} },
    BackHandler: {
      addEventListener: (_event: string, handler: () => boolean) => {
        backHandlers.push(handler);
        return { remove: () => backHandlers.splice(backHandlers.indexOf(handler), 1) };
      },
    },
    Keyboard: { dismiss: () => dismissed.push("keyboard") },
    Platform: { OS: "android" },
    Pressable: host("pressable"),
    ScrollView: host("scroll"),
    StyleSheet: { create: (value: unknown) => value, hairlineWidth: 1, flatten: (value: unknown) => value, absoluteFill: {} },
    Text: host("text"),
    TextInput: host("input"),
    View: host("view"),
    findNodeHandle: () => 7,
    useWindowDimensions: () => ({ width: 390, height: 844 }),
  }));
  // Shared values are plain boxes; an animation that starts has finished.
  const progress = { value: 0 };
  let sharedValues = 0;
  mock.module("react-native-reanimated", () => ({
    default: { View: host("animatedView") },
    Easing: { out: (fn: unknown) => fn, in: (fn: unknown) => fn, bezier: () => 1, quad: 1 },
    runOnJS: (fn: () => void) => fn,
    useAnimatedProps: (worklet: () => unknown) => worklet(),
    useAnimatedStyle: (worklet: () => unknown) => worklet(),
    useSharedValue: (value: number) => {
      const ref = React.useRef<{ value: number }>(null as never);
      if (!ref.current) ref.current = sharedValues++ === 0 ? progress : { value };
      return ref.current;
    },
    withSpring: (value: number) => value,
    withTiming: (value: number) => value,
  }));
  mock.module("react-native-gesture-handler", () => {
    const pan: any = new Proxy({}, { get: () => () => pan });
    return { Gesture: { Pan: () => pan }, GestureDetector: host("detector") };
  });
  mock.module("react-native-safe-area-context", () => ({ useSafeAreaInsets: () => ({ top: 0, bottom: 0, left: 0, right: 0 }) }));
  mock.module("../../constants/motion", () => ({ Spring: { rise: {} } }));
  mock.module("../../constants/tokens", () => ({
    Radii: { sheet: 24 },
    Typography: {},
    TypeScale: new Proxy({}, { get: () => ({}) }),
    useAppColors: () => ({ modalBackdrop: "dim", borderStrong: "line" }),
  }));
  mock.module("../../theme/provider", () => ({ TerminalThemeScope: host("scope") }));
  mock.module("../ui/GlassSurface", () => ({ GlassSurface: host("glass") }));
  mock.module("../terminal/composerInputBehavior", () => ({ webReplyKeyPress: () => null }));
  mock.module("../terminal/InterfaceComposerAttachmentRail", () => ({ InterfaceComposerAttachmentRail: none }));
  mock.module("../terminal/InterfaceDropOverlay", () => ({ InterfaceDropOverlay: none }));
  mock.module("../terminal/useComposerFileTransfer", () => ({ useComposerFileTransfer: () => ({ dragging: false }) }));
  mock.module("../terminal/useInterfaceComposerAttachments", () => ({ useInterfaceComposerAttachments: () => ({}) }));
  mock.module("../navigation/useDesktopWeb", () => ({ useDesktopWeb: () => false }));
  mock.module("../navigation/ResizeHandle", () => ({ ResizeHandle: none, useResizableWidth: () => ({}) }));
  mock.module("../navigation/primaryAppBarGeometry", () => ({ PRIMARY_APP_BAR_HEIGHT: 56 }));
  mock.module("../navigation/primaryAppBarInset", () => ({ usePrimaryAppBarLayout: () => ({}) }));
  mock.module("../pets/PetSprite", () => ({ PetSprite: none }));
  mock.module("../icons/Icon", () => ({ Icon: none }));
  mock.module("../ui/IconButton", () => ({ IconButton: host("iconButton") }));
  mock.module("../ui/StatusPill", () => ({ StatusPill: none }));
  mock.module("../ui/BottomSheetFrame", () => ({ BottomSheetFrame: none }));
  mock.module("../terminal/composerMaterial", () => ({ composerNeutralFill: () => "#fff" }));
  mock.module("../calendar/CalendarOriginLink", () => ({ CalendarOriginLink: none }));
  // Every render of a slip is counted: that is the sheet's subtree.
  let slipRenders = 0;
  const rendersOf: Record<string, number> = {};
  mock.module("./WorkSlip", () => ({
    WORK_STATUS_TONE: {},
    WorkSlip: (props: any) => {
      slipRenders += 1;
      const id = String(props.title).slice(0, 6);
      rendersOf[id] = (rendersOf[id] ?? 0) + 1;
      return React.createElement("slip", props);
    },
    WorkSlipActions: none,
  }));

  const { BrainWorkOverlayHost, HideWhenWorkOpen } = await import("./BrainWorkOverlay");
  const { brainWorkOverlay } = await import("./brainWorkOverlayStore");
  const { useBrainWorkOverlayContent, sameWorkSurface } = await import("./useBrainWorkOverlayContent");

  const slip = (workId: string, group: string) => ({
    workId,
    group,
    status: "waiting",
    statusLabel: "Needs you",
    title: `Work ${workId}`,
    updatedAt: "2026-11-08T10:00:00.000Z",
    waitedMs: 1000,
  });
  const makeSurface = (waitedMs = 1000) =>
    ({
      slips: [slip("a", "needs"), slip("b", "needs"), slip("c", "running")].map((item) => ({ ...item, waitedMs })),
      counts: { needs: 2, running: 1, back: 0, waiting: 0 },
    }) as any;
  const chrome = { appBackground: "paper", text: "ink", textMuted: "soft", surface: "paper" } as any;
  const onOpenSlip = () => {};
  const content = (surface = makeSurface()) => ({ surface, chrome, onOpenSlip }) as any;
  const titles = (renderer: TestRenderer.ReactTestRenderer) =>
    renderer.root.findAllByType("slip" as any).map((item) => item.props.title);

  async function mount() {
    progress.value = 0;
    sharedValues = 0;
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(<BrainWorkOverlayHost />);
    });
    return renderer;
  }

  test("a shut sheet is mounted with every slip drawn; opening moves the shared value and renders no slip", async () => {
    const renderer = await mount();
    await act(async () => brainWorkOverlay.publish(content()));
    // Shut, hidden from screen readers and from touches, and already drawn.
    expect(titles(renderer)).toEqual(["Work a", "Work b", "Work c"]);
    expect(progress.value).toBe(0);
    expect(renderer.root.findByType("animatedView" as any).props.importantForAccessibility).toBe("no-hide-descendants");
    expect(renderer.root.findByType("animatedView" as any).props.animatedProps.pointerEvents).toBe("none");
    expect(renderer.root.findAllByType("pressable" as any)).toHaveLength(0);

    const slipsBefore = slipRenders;
    act(() => brainWorkOverlay.open());
    // The open path is the shared value; no slip rendered because of it.
    expect(progress.value).toBe(1);
    expect(slipRenders).toBe(slipsBefore);
    expect(titles(renderer)).toHaveLength(3);
    // What follows the flip is the wrapper's own accessibility and the back button.
    const root = renderer.root.findByType("animatedView" as any);
    expect(root.props.importantForAccessibility).toBe("yes");
    expect(root.props.accessibilityViewIsModal).toBe(true);
    expect(root.props.animatedProps.pointerEvents).toBe("box-none");
    expect(root.props.pointerEvents).toBe("box-none");
    // The backdrop takes the tap that closes it only while open.
    expect(renderer.root.findAllByType("pressable" as any)).toHaveLength(1);
    expect(dismissed).toContain("keyboard");
    expect(backHandlers).toHaveLength(1);

    // Android back closes it, and the sheet follows.
    let handled = false;
    act(() => {
      handled = backHandlers[0]();
    });
    expect(handled).toBe(true);
    expect(brainWorkOverlay.getOpen()).toBe(false);
    expect(progress.value).toBe(0);
    expect(backHandlers).toHaveLength(0);
    expect(slipRenders).toBe(slipsBefore);
    await act(async () => brainWorkOverlay.publish(null));
  });

  test("the rest of the app is hidden from screen readers only while the sheet is open", async () => {
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(
        <>
          <HideWhenWorkOpen>{React.createElement("app")}</HideWhenWorkOpen>
          <BrainWorkOverlayHost />
        </>,
      );
    });
    await act(async () => brainWorkOverlay.publish(content()));
    const app = () => renderer.root.findByType("app" as any).parent!;
    expect(app().props.accessibilityElementsHidden).toBe(false);
    act(() => brainWorkOverlay.open());
    expect(app().props.accessibilityElementsHidden).toBe(true);
    expect(app().props.importantForAccessibility).toBe("no-hide-descendants");
    act(() => brainWorkOverlay.close());
    expect(app().props.accessibilityElementsHidden).toBe(false);
    await act(async () => brainWorkOverlay.publish(null));
  });

  test("a Brain event that rebuilds the screen's objective and callbacks redraws no slip; a changed Work or action does", async () => {
    let busy = false;
    const pressed: string[] = [];
    // The screen's own: new identities on every render, as the real screen makes them per event.
    function Screen({ surface, objective }: { surface: any; objective: any }) {
      useBrainWorkOverlayContent({
        surface,
        objective,
        chrome,
        onOpenSlip: (item: any) => pressed.push(`open ${item.workId}`),
        actionsFor: (item: any) => [{ key: "reply", label: "Reply", busy, onPress: () => pressed.push(`reply ${item.workId}`) }],
      } as any);
      return null;
    }
    const renderer = await mount();
    let screen!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      screen = TestRenderer.create(<Screen surface={makeSurface(1000)} objective={{ goal: "Ship it", back: 1, total: 3 }} />);
    });
    const drawn = slipRenders;
    expect(drawn).toBeGreaterThan(0);
    // The store moved and the clock did: new objects, `waitedMs` later, an equal objective, new callbacks: nothing to see.
    await act(async () =>
      screen.update(<Screen surface={makeSurface(61_000)} objective={{ goal: "Ship it", back: 1, total: 3 }} />),
    );
    expect(slipRenders).toBe(drawn);
    expect(sameWorkSurface(makeSurface(1), makeSurface(2))).toBe(true);

    // A button going busy is a change the slips must show.
    busy = true;
    await act(async () => screen.update(<Screen surface={makeSurface(62_000)} objective={{ goal: "Ship it", back: 1, total: 3 }} />));
    expect(slipRenders).toBeGreaterThan(drawn);
    const slipProps = renderer.root.findAllByType("slip" as any)[0].props;
    expect(slipProps.actions[0].busy).toBe(true);
    // A press reaches the screen's latest handler, not the one the slip was drawn with.
    act(() => slipProps.actions[0].onPress());
    act(() => slipProps.onPress());
    expect(pressed).toEqual(["reply a", "open a"]);

    const changed = makeSurface();
    changed.slips[0].title = "Work a, renamed";
    expect(sameWorkSurface(makeSurface(), changed)).toBe(false);
    const before = { ...rendersOf };
    await act(async () => screen.update(<Screen surface={changed} objective={{ goal: "Ship it", back: 1, total: 3 }} />));
    expect(titles(renderer)[0]).toBe("Work a, renamed");
    // A Work update redraws the slip it touched and no other.
    expect(rendersOf["Work b"]).toBe(before["Work b"]);
    expect(rendersOf["Work c"]).toBe(before["Work c"]);
    expect(rendersOf["Work a"]).toBeGreaterThan(before["Work a"]);
    // The screen going away takes the sheet with it.
    await act(async () => screen.unmount());
    expect(renderer.root.findAllByType("animatedView" as any)).toHaveLength(0);
  });
}
