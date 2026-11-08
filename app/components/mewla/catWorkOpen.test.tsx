import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.MEWLA_CAT_WORK_OPEN_CHILD) {
  test("the cat row's hint opens the Work sheet without rendering the screen", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_CAT_WORK_OPEN_CHILD: "1" },
    });
    if (result.exitCode) {
      throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) => React.createElement(name, props, props.children);

  mock.module("react-native", () => ({
    AccessibilityInfo: { announceForAccessibility: () => {} },
    StyleSheet: { create: (value: unknown) => value, hairlineWidth: 1, flatten: (value: unknown) => value },
    Text: host("text"),
    View: host("view"),
    Pressable: host("pressable"),
  }));
  mock.module("../../constants/tokens", () => ({
    TypeScale: { label: {}, caption: {} },
    useAppTheme: () => ({ theme: { materials: { tint: "tint" } } }),
  }));
  mock.module("../icons/Icon", () => ({ Icon: host("icon") }));
  // The pill's press state is a tap gesture's worklets driving shared values; keep the handlers to call them.
  const tapHandlers: { onBegin?: () => void; onFinalize?: () => void; hitSlop?: unknown; minDuration?: number } = {};
  const sharedValues: { value: number }[] = [];
  mock.module("react-native-gesture-handler", () => {
    const tap: any = {
      minDuration(value: number) {
        tapHandlers.minDuration = value;
        return tap;
      },
      hitSlop(value: unknown) {
        tapHandlers.hitSlop = value;
        return tap;
      },
      onBegin(fn: () => void) {
        tapHandlers.onBegin = fn;
        return tap;
      },
      onFinalize(fn: () => void) {
        tapHandlers.onFinalize = fn;
        return tap;
      },
    };
    return { Gesture: { LongPress: () => tap }, GestureDetector: host("detector") };
  });
  mock.module("react-native-reanimated", () => ({
    default: { View: host("animatedView") },
    Easing: { out: (fn: unknown) => fn, quad: 1 },
    useSharedValue: (value: number) => {
      const ref = React.useRef({ value });
      sharedValues[0] = ref.current;
      return ref.current;
    },
    useAnimatedStyle: (worklet: () => unknown) => worklet(),
    withTiming: (value: number) => value,
  }));
  mock.module("./TappableCat", () => ({ TappableCat: host("cat") }));
  mock.module("../brain/BrainWorkPanel", () => ({
    BrainWorkSheet: (props: any) => React.createElement("sheet", props),
  }));

  const chrome = { text: "ink", textMuted: "soft", seal: "seal", onSeal: "paper" } as any;
  const { BrainCatRow } = await import("./BrainCompanion");
  const { BrainWorkSheetHost } = await import("../brain/BrainWorkSheetHost");

  const surface = { slips: [], counts: { needs: 0, running: 0, back: 0, waiting: 0 } } as any;
  const companion = (state: any, overrides: any = {}) => ({ presence: { state, count: 4 }, animate: false, ...overrides });

  function press(renderer: TestRenderer.ReactTestRenderer) {
    const target = renderer.root.findByType("pressable" as any);
    act(() => target.props.onPress());
  }

  test("pressing the hint opens the sheet through the host, and the screen never renders", async () => {
    let screenRenders = 0;
    const handle: { current: any } = { current: null };
    function Screen() {
      screenRenders += 1;
      const c = companion("attention", { onOpenWork: () => handle.current?.open() });
      return (
        <>
          <BrainCatRow companion={c} label="4 need you" chrome={chrome} onPress={c.onOpenWork} />
          <BrainWorkSheetHost ref={handle} surface={surface} chrome={chrome} onOpenSlip={() => {}} />
        </>
      );
    }
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(<Screen />);
    });
    const rendersBefore = screenRenders;
    expect(renderer.root.findByType("sheet" as any).props.visible).toBe(false);
    press(renderer);
    expect(renderer.root.findByType("sheet" as any).props.visible).toBe(true);
    // Opening the list renders the sheet only: the chat, composer and menus wait for nothing.
    expect(screenRenders).toBe(rendersBefore);
    act(() => renderer.root.findByType("sheet" as any).props.onClose());
    expect(renderer.root.findByType("sheet" as any).props.visible).toBe(false);
  });

  test("the wide Work column keeps the sheet shut, and closes one that was open", async () => {
    const handle: { current: any } = { current: null };
    const tree = (suppressed: boolean) => (
      <BrainWorkSheetHost ref={handle} suppressed={suppressed} surface={surface} chrome={chrome} onOpenSlip={() => {}} />
    );
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(tree(false));
    });
    act(() => handle.current.open());
    expect(renderer.root.findByType("sheet" as any).props.visible).toBe(true);
    await act(async () => renderer.update(tree(true)));
    expect(renderer.root.findByType("sheet" as any).props.visible).toBe(false);
    act(() => handle.current.open());
    expect(renderer.root.findByType("sheet" as any).props.visible).toBe(false);
    // The column going away does not bring the old sheet back.
    await act(async () => renderer.update(tree(false)));
    expect(renderer.root.findByType("sheet" as any).props.visible).toBe(false);
  });

  test("the hint says how it looks by state: seal for needs you, tint for counts, words when quiet", async () => {
    const look = async (state: string, label: string) => {
      let renderer!: TestRenderer.ReactTestRenderer;
      await act(async () => {
        renderer = TestRenderer.create(
          <BrainCatRow companion={companion(state) as any} label={label} chrome={chrome} onPress={() => {}} />,
        );
      });
      const button = renderer.root.findByType("pressable" as any);
      const style = (button.props.style as any[]).flat(Infinity).filter(Boolean);
      return {
        background: style.find((item) => item.backgroundColor)?.backgroundColor,
        chevron: renderer.root.findAllByType("icon" as any).length === 1,
        role: button.props.accessibilityRole,
        slop: button.props.hitSlop,
      };
    };
    const needs = await look("attention", "4 need you");
    expect(needs).toMatchObject({ background: "seal", chevron: true, role: "button" });
    expect(await look("delegating", "3 Workers running")).toMatchObject({ background: "tint", chevron: true });
    const quiet = await look("idle", "All quiet");
    expect(quiet).toMatchObject({ background: undefined, chevron: false, role: "button" });
    // 32 pt drawn plus the slop is at least a 44 pt target, at every tone.
    for (const result of [needs, quiet]) {
      expect(32 + result.slop.top + result.slop.bottom).toBeGreaterThanOrEqual(44);
    }
  });

  test("the cat's answer replaces the hint's words for a moment and the hint stays pressable", async () => {
    const onOpenWork = mock(() => {});
    const c = companion("idle", { onCatTap: () => "All quiet. Nothing out, nothing needs you." });
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(<BrainCatRow companion={c as any} label="All quiet" chrome={chrome} onPress={onOpenWork} />);
    });
    act(() => renderer.root.findByType("cat" as any).props.onPress());
    const text = renderer.root.findByType("text" as any);
    expect(text.props.children).toBe("All quiet. Nothing out, nothing needs you.");
    press(renderer);
    expect(onOpenWork).toHaveBeenCalledTimes(1);
  });

  test("the pill answers the finger from a gesture's worklets, not from the Pressable's JS press events", async () => {
    const c = companion("attention");
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(<BrainCatRow companion={c as any} label="4 need you" chrome={chrome} onPress={() => {}} />);
    });
    // The press itself and its accessibility stay on the Pressable.
    const button = renderer.root.findByType("pressable" as any);
    expect(button.props.onPressIn).toBeUndefined();
    expect(button.props.accessibilityRole).toBe("button");
    // On Android and iOS a gesture that activates cancels the JS responder, so the Pressable would get a
    // cancel instead of onPress and the list would never open. A Tap activates on lift; this one must not.
    expect(tapHandlers.minDuration).toBeGreaterThanOrEqual(60_000);
    // The gesture covers the same 46 pt target the Pressable does.
    expect(tapHandlers.hitSlop).toEqual(button.props.hitSlop);
    const shade = () => {
      const views = renderer.root.findAllByType("animatedView" as any);
      return views.find((view) => view.props.pointerEvents === "none")!.props.style.flat(Infinity).filter(Boolean);
    };
    const opacity = () => shade().find((item: any) => item.opacity !== undefined).opacity;
    expect(opacity()).toBe(0);
    // Finger down: the shade is on at once, with no JS press event involved.
    act(() => tapHandlers.onBegin!());
    await act(async () => renderer.update(<BrainCatRow companion={c as any} label="4 need you" chrome={chrome} onPress={() => {}} />));
    expect(opacity()).toBeGreaterThan(0.15);
    act(() => tapHandlers.onFinalize!());
    await act(async () => renderer.update(<BrainCatRow companion={c as any} label="4 need you" chrome={chrome} onPress={() => {}} />));
    expect(opacity()).toBe(0);
  });

  test("a running turn's Working row has no hint to press", async () => {
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(
        <BrainCatRow companion={companion("working") as any} label="Working" detail="Running a command" chrome={chrome} onPress={() => {}} turnRunning />,
      );
    });
    expect(renderer.root.findAllByType("pressable" as any)).toHaveLength(0);
  });
}
