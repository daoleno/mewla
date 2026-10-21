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
  mock.module("react-native", () => ({
    Platform: { OS: "ios" },
    Pressable: "Pressable",
    View: "View",
    StyleSheet: { create: (styles: unknown) => styles },
  }));
  mock.module("expo-clipboard", () => ({ setStringAsync: async () => {} }));
  mock.module("expo-haptics", () => ({
    impactAsync: async () => {},
    ImpactFeedbackStyle: { Medium: "medium" },
  }));
  mock.module("../icons/Icon", () => ({ Icon: "Icon" }));
  mock.module("../navigation/useDesktopWeb", () => ({ useDesktopWeb: () => desktop }));
  const { MessageActionSurface, MessageActionsContext } = await import("./MessageActions");
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
  const environment = { messageRewind: true, brain: false, running: false, connected: true, canSelectText: true };
  // The timeline's own handlers, which a selectable Text would use as its long press.
  const timelineGesture = { selectable: true, onTextSelectionGestureStart() {}, onTextSelectionGestureEnd() {} };

  function render(selectingId: string | null, opened: string[] = []) {
    let seen: ReturnType<typeof useTimelineSelectableTextProps> | undefined;
    function Child() {
      seen = useTimelineSelectableTextProps();
      return null;
    }
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
                environment,
                selectingId,
                openMenu: (s: { id: string }) => opened.push(s.id),
                run: () => {},
              },
            },
            React.createElement(MessageActionSurface, {
              subject,
              align: "end",
              chrome: {} as any,
              children: React.createElement(Child),
            }),
          ),
        ),
      );
    });
    return { seen: () => seen!, tree: renderer };
  }

  test("on the phone a long press on the text reaches the menu, not a selectable Text", () => {
    desktop = false;
    const view = render(null);
    expect(view.seen().selectable).toBe(false);
    // A Text carrying these would become the touch responder and swallow the press.
    expect(view.seen().onLongPress).toBeUndefined();
    expect(view.seen().onPressOut).toBeUndefined();
  });

  test("the long press opens the menu for that message", () => {
    desktop = false;
    const opened: string[] = [];
    const view = render(null, opened);
    const pressable = view.tree.root.findByType("Pressable" as any);
    act(() => pressable.props.onLongPress());
    expect(opened).toEqual(["u1"]);
  });

  test("Select text hands selection and its gestures back for that message only", () => {
    desktop = false;
    const selecting = render("u1");
    expect(selecting.seen().selectable).toBe(true);
    expect(selecting.seen().onLongPress).toBe(timelineGesture.onTextSelectionGestureStart);
    // While selecting, the wrapper steps aside so the OS selection long press works.
    expect(selecting.tree.root.findByType("Pressable" as any).props.onLongPress).toBeUndefined();
    const other = render("someone-else");
    expect(other.seen().selectable).toBe(false);
  });

  test("on desktop the timeline keeps native selection", () => {
    desktop = true;
    const view = render(null);
    expect(view.seen().selectable).toBe(true);
  });
}
