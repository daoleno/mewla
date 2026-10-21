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
  const opened: string[] = [];
  const gestureConfig: { enabled?: boolean; minDuration?: number; onStart?: () => void } = {};
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
      onStart: (callback: () => void) => ((gestureConfig.onStart = callback), builder),
    };
    return { Gesture: { LongPress: () => builder }, GestureDetector: "GestureDetector" };
  });
  mock.module("expo-clipboard", () => ({ setStringAsync: async () => {} }));
  mock.module("expo-haptics", () => ({
    impactAsync: async () => {},
    ImpactFeedbackStyle: { Medium: "medium" },
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
    options: { calls?: string[]; env?: typeof environment; latestReply?: { replyId: string; prompt: typeof subject } } = {},
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
                latestReply: options.latestReply,
                openMenu: (m: { id: string }) => opened.push(m.id),
                copy: (m: { id: string }) => calls.push(`copy ${m.id}`),
                select: (m: { id: string }) => calls.push(`select ${m.id}`),
                edit: (m: { id: string }) => calls.push(`edit ${m.id}`),
                retry: (m: { id: string }) => calls.push(`retry ${m.id}`),
              },
            },
            React.createElement(MessageActionSurface, {
              subject: message,
              align: message.role === "user" ? "end" : "start",
              maxWidth: "86%",
              chrome: {} as any,
              children: React.createElement(Child),
            }),
            React.createElement(ReplyActionRow, { subject: message, chrome: {} as any }),
          ),
        ),
      );
    });
    return { seen: () => seen!, tree: renderer };
  }
  const pressable = (view: { tree: TestRenderer.ReactTestRenderer }) => view.tree.root.findByType("Pressable" as any);

  test("on the phone a long press on your message opens its sheet; the text does not start its own selection", () => {
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
    act(() => pressable(view).props.onLongPress());
    expect(opened).toEqual(["u1"]);
  });

  test("the gesture-handler path opens the sheet too, and both paths for one press open it once", () => {
    desktop = false;
    platform.OS = "ios";
    opened.length = 0;
    const view = render(subject);
    act(() => gestureConfig.onStart!());
    expect(opened).toEqual(["u1"]);
    act(() => pressable(view).props.onLongPress());
    expect(opened).toEqual(["u1"]);
  });

  test("a reply opens the same sheet on both phones", () => {
    desktop = false;
    for (const os of ["ios", "android"]) {
      platform.OS = os;
      opened.length = 0;
      const view = render(reply);
      expect(view.seen().selectable).toBe(false);
      act(() => pressable(view).props.onLongPress());
      expect(opened).toEqual(["a1"]);
    }
  });

  test("a streaming reply or an empty message has no sheet and keeps the timeline's text rules", () => {
    desktop = false;
    platform.OS = "ios";
    opened.length = 0;
    const streaming = render({ ...reply, streaming: true });
    expect(pressable(streaming).props.onLongPress).toBeUndefined();
    expect(gestureConfig.enabled).toBe(false);
    expect(streaming.seen().selectable).toBe(true);
    expect(pressable(render({ ...subject, body: " " })).props.onLongPress).toBeUndefined();
  });

  test("on desktop the timeline keeps native selection and shows the hover row", () => {
    desktop = true;
    platform.OS = "web";
    const view = render(subject);
    expect(view.tree.root.findAllByType("GestureDetector" as any)).toHaveLength(0);
    expect(view.seen().selectable).toBe(true);
    expect(view.tree.root.findByProps({ accessibilityLabel: "Edit message" })).toBeDefined();
  });

  test("the newest reply shows Copy and Retry; older replies and busy Sessions do not", () => {
    desktop = false;
    platform.OS = "ios";
    const latestReply = { replyId: "a1", prompt: subject };
    const calls: string[] = [];
    const view = render(reply, { latestReply, calls });
    const retry = view.tree.root.findByProps({ accessibilityLabel: "Retry" });
    view.tree.root.findByProps({ accessibilityLabel: "Copy" });
    act(() => retry.props.onPress());
    expect(calls).toEqual(["retry u1"]);

    expect(render({ ...reply, id: "a0" }, { latestReply }).tree.root.findAllByProps({ accessibilityLabel: "Retry" })).toHaveLength(0);
    const busy = render(reply, { latestReply, env: { ...environment, running: true } });
    expect(busy.tree.root.findAllByProps({ accessibilityLabel: "Retry" })).toHaveLength(0);
    busy.tree.root.findByProps({ accessibilityLabel: "Copy" });
  });
}
