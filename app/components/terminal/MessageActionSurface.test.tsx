import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.MEWLA_MESSAGE_SURFACE_TEST) {
  test("message surface selection in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_MESSAGE_SURFACE_TEST: "1" },
    });
    if (result.exitCode) {
      throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  let desktop = false;
  const platform = { OS: "ios" };
  const opened: { id: string; rect: unknown; touchY: number; align: string }[] = [];
  const gestureConfig: { enabled?: boolean; minDuration?: number; onStart?: (event: { x: number; y: number; absoluteX: number; absoluteY: number }) => void } = {};
  mock.module("react-native", () => ({
    Platform: platform,
    Pressable: "Pressable",
    View: "View",
    StyleSheet: { create: (styles: unknown) => styles },
  }));
  mock.module("react-native-gesture-handler", () => {
    const builder: any = {
      runOnJS: () => builder,
      enabled: (value: boolean) => ((gestureConfig.enabled = value), builder),
      minDuration: (value: number) => ((gestureConfig.minDuration = value), builder),
      maxDistance: () => builder,
      onStart: (callback: (event: { x: number; y: number; absoluteX: number; absoluteY: number }) => void) => ((gestureConfig.onStart = callback), builder),
    };
    return { Gesture: { LongPress: () => builder }, GestureDetector: "GestureDetector" };
  });
  mock.module("expo-clipboard", () => ({ setStringAsync: async () => {} }));
  mock.module("expo-haptics", () => ({
    impactAsync: async () => {},
    ImpactFeedbackStyle: { Light: "light" },
  }));
  mock.module("../icons/Icon", () => ({ Icon: "Icon" }));
  mock.module("../navigation/useDesktopWeb", () => ({ useDesktopWeb: () => desktop }));
  const { MessageActionSurface, MessageActionsContext, ReplyActionRow } = await import("./MessageActions");
  const { TimelineTextSelectableContext, useTimelineSelectableTextProps } = await import(
    "./TimelineTextSelectableContext"
  );

  const subject = {
    id: "u1",
    role: "user" as const,
    body: "ship it",
    hasAttachments: false,
    pending: false,
    streaming: false,
  };
  const reply = { ...subject, id: "a1", role: "assistant" as const, body: "**done**" };
  const environment = { messageRewind: true, brain: false, running: false, connected: true, canSelectText: true };
  // The timeline's own handlers, which a selectable Text would use as its long press.
  const timelineGesture = { selectable: true, onTextSelectionGestureStart() {}, onTextSelectionGestureEnd() {} };

  function render(
    message: typeof subject | typeof reply,
    options: { calls?: string[]; env?: typeof environment; replyTurns?: Map<string, any>; activeId?: string } = {},
  ) {
    let seen: ReturnType<typeof useTimelineSelectableTextProps> | undefined;
    function Child() {
      seen = useTimelineSelectableTextProps();
      return null;
    }
    const calls = options.calls ?? [];
    let renderer!: TestRenderer.ReactTestRenderer;
    act(() => {
      renderer = TestRenderer.create(
        React.createElement(
          TimelineTextSelectableContext.Provider,
          { value: timelineGesture },
          React.createElement(
            MessageActionsContext.Provider,
            {
              value: {
                environment: options.env ?? environment,
                replyTurns: options.replyTurns ?? new Map(),
                activeId: options.activeId ?? null,
                openMenu: (m: { id: string }, anchor: { rect: unknown; touchY: number; align: string }) =>
                  opened.push({ id: m.id, ...anchor }),
                copy: (m: { id: string }) => calls.push(`copy ${m.id}`),
                select: (m: { id: string }) => calls.push(`select ${m.id}`),
                edit: (m: { id: string }) => calls.push(`edit ${m.id}`),
                retry: (turn: { prompt?: { id: string } }) => calls.push(`retry ${turn.prompt?.id}`),
              },
            },
            message.role === "user"
              ? React.createElement(MessageActionSurface, {
                  subject: message,
                  align: "end",
                  maxWidth: "86%",
                  chrome: {} as any,
                  children: React.createElement(Child),
                })
              : React.createElement(Child),
            React.createElement(ReplyActionRow, { subject: message, chrome: {} as any }),
          ),
        ),
        // Where the pressed view is in the window.
        { createNodeMock: () => ({ measureInWindow: (done: (...v: number[]) => void) => done(40, 300, 200, 48) }) },
      );
    });
    return { seen: () => seen!, tree: renderer };
  }
  const turnFor = (message: { id: string }, extra: Record<string, unknown> = {}) =>
    new Map([[message.id, { replyId: message.id, prompt: subject, latest: true, settled: true, ...extra }]]);

  test("on the phone a long press on your message opens the menu beside it; the text does not start its own selection", async () => {
    desktop = false;
    platform.OS = "android";
    opened.length = 0;
    const view = render(subject);
    expect(view.seen().selectable).toBe(false);
    // A Text carrying these would become the touch responder and swallow the press.
    expect(view.seen().onLongPress).toBeUndefined();
    expect(view.seen().onPressOut).toBeUndefined();
    expect(gestureConfig.enabled).toBe(true);
    expect(gestureConfig.minDuration).toBe(350);
    // The measure only gives the size: where the view is comes from the touch,
    // since a measure inside the inverted list is mirrored.
    await act(async () => gestureConfig.onStart!({ x: 20, y: 10, absoluteX: 60, absoluteY: 320 }));
    expect(opened).toEqual([
      { id: "u1", rect: { x: 40, y: 310, width: 200, height: 48 }, touchY: 320, align: "end" },
    ]);
  });

  test("the pressed message is highlighted while its menu is open", () => {
    desktop = false;
    platform.OS = "ios";
    expect(render(subject).tree.root.findAllByProps({ pointerEvents: "none" })).toHaveLength(0);
    expect(render(subject, { activeId: "u1" }).tree.root.findAllByProps({ pointerEvents: "none" })).toHaveLength(1);
  });

  test("a reply has no long press: its text keeps the system selection and the timeline's freeze handlers", () => {
    desktop = false;
    for (const os of ["ios", "android"]) {
      platform.OS = os;
      const view = render(reply);
      expect(view.tree.root.findAllByType("GestureDetector" as any)).toHaveLength(0);
      expect(view.seen()).toEqual({
        selectable: true,
        onLongPress: timelineGesture.onTextSelectionGestureStart,
        onPressOut: timelineGesture.onTextSelectionGestureEnd,
      });
    }
  });

  test("an empty or still-sending message has no menu and keeps the timeline's text rules", () => {
    desktop = false;
    platform.OS = "ios";
    const empty = render({ ...subject, body: " " });
    expect(gestureConfig.enabled).toBe(false);
    expect(empty.seen().selectable).toBe(true);
  });

  test("on desktop the timeline keeps native selection and shows the hover row", () => {
    desktop = true;
    platform.OS = "web";
    const view = render(subject);
    expect(view.tree.root.findAllByType("GestureDetector" as any)).toHaveLength(0);
    expect(view.seen().selectable).toBe(true);
    expect(view.tree.root.findByProps({ accessibilityLabel: "Edit message" })).toBeDefined();
  });

  // A finger lands, jitters a little, lifts: what a real touch does and a bare tap does not.
  const touch = (pageX: number, pageY: number, locationX = 18, locationY = 16) => ({
    nativeEvent: { pageX, pageY, locationX, locationY },
  });
  const finger = (node: { props: any }, moves: [number, number][] = [[1, 1]], at: [number, number] = [70, 500]) => {
    act(() => node.props.onResponderGrant(touch(at[0], at[1])));
    for (const [dx, dy] of moves) act(() => node.props.onResponderMove(touch(at[0] + dx, at[1] + dy)));
    act(() => node.props.onResponderRelease());
  };

  test("every reply with a turn shows Copy; Retry only where the prompt can run again", () => {
    desktop = false;
    platform.OS = "ios";
    const calls: string[] = [];
    const view = render(reply, { replyTurns: turnFor(reply, { latest: false }), calls });
    const retry = view.tree.root.findByProps({ accessibilityLabel: "Retry" });
    view.tree.root.findByProps({ accessibilityLabel: "Copy" });
    finger(retry);
    expect(calls).toEqual(["retry u1"]);
    // Past the touch slop it is a drag, not a press; a system takeover cancels it.
    finger(retry, [[0, 30]]);
    act(() => retry.props.onResponderGrant(touch(70, 500)));
    act(() => retry.props.onResponderTerminate());
    act(() => retry.props.onResponderRelease());
    expect(calls).toEqual(["retry u1"]);
    expect(retry.props.hitSlop).toBeDefined();
    expect(retry.props.style[0].width).toBeGreaterThanOrEqual(44);
    expect(retry.props.style[0].height).toBeGreaterThanOrEqual(44);

    // A message that is not a turn's final reply has no row.
    expect(render({ ...reply, id: "a0" }, { replyTurns: turnFor(reply) }).tree.root.findAllByProps({ accessibilityLabel: "Copy" })).toHaveLength(0);
    for (const hidden of [
      render(reply, { replyTurns: turnFor(reply), env: { ...environment, running: true } }),
      render(reply, { replyTurns: turnFor(reply, { settled: false }) }),
      render(reply, { replyTurns: turnFor(reply, { prompt: undefined }) }),
    ]) {
      expect(hidden.tree.root.findAllByProps({ accessibilityLabel: "Retry" })).toHaveLength(0);
      hidden.tree.root.findByProps({ accessibilityLabel: "Copy" });
    }
  });

  test("a streaming reply has no row", () => {
    desktop = false;
    platform.OS = "ios";
    const view = render({ ...reply, streaming: true }, { replyTurns: turnFor(reply) });
    expect(view.tree.root.findAllByProps({ accessibilityLabel: "Copy" })).toHaveLength(0);
  });

  test("the reply row's More opens the menu under its button, and is a phone-only control", async () => {
    desktop = false;
    platform.OS = "android";
    opened.length = 0;
    const view = render(reply, { replyTurns: turnFor(reply) });
    const more = view.tree.root.findByProps({ accessibilityLabel: "More" });
    act(() => more.props.onResponderGrant(touch(70, 500, 10, 8)));
    act(() => more.props.onResponderMove(touch(72, 502, 10, 8)));
    await act(async () => more.props.onResponderRelease());
    expect(opened).toMatchObject([{ id: "a1", align: "start", rect: { x: 60, y: 492, width: 200, height: 48 } }]);

    desktop = true;
    platform.OS = "web";
    const web = render(reply, { replyTurns: turnFor(reply), env: { ...environment, canSelectText: false } });
    expect(web.tree.root.findAllByProps({ accessibilityLabel: "More" })).toHaveLength(0);
  });
}
