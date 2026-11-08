import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.MEWLA_BRAIN_WORK_SHEET_CHILD) {
  test("the Work sheet draws its slips on the open, in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_BRAIN_WORK_SHEET_CHILD: "1" },
    });
    if (result.exitCode) {
      throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const host = (name: string) => (props: any) => React.createElement(name, props, props.children);
  // A frame never arrives in this test: whatever waits for one is not in the first commit.
  Object.assign(globalThis, { requestAnimationFrame: () => 0, cancelAnimationFrame: () => {} });
  const none = () => null;

  mock.module("react-native", () => ({
    Platform: { OS: "android" },
    Pressable: host("pressable"),
    ScrollView: host("scroll"),
    StyleSheet: { create: (value: unknown) => value, hairlineWidth: 1, flatten: (value: unknown) => value, absoluteFill: {} },
    Text: host("text"),
    TextInput: host("input"),
    View: host("view"),
  }));
  mock.module("../ui/BottomSheetFrame", () => ({
    // A closed frame draws nothing, as the real one does.
    BottomSheetFrame: (props: any) => (props.visible ? React.createElement("frame", props, props.children) : null),
  }));
  mock.module("../terminal/composerInputBehavior", () => ({ webReplyKeyPress: () => null }));
  mock.module("../terminal/InterfaceComposerAttachmentRail", () => ({ InterfaceComposerAttachmentRail: none }));
  mock.module("../terminal/InterfaceDropOverlay", () => ({ InterfaceDropOverlay: none }));
  mock.module("../terminal/useComposerFileTransfer", () => ({ useComposerFileTransfer: () => ({ dragging: false }) }));
  mock.module("../terminal/useInterfaceComposerAttachments", () => ({ useInterfaceComposerAttachments: () => ({}) }));
  mock.module("../navigation/useDesktopWeb", () => ({ useDesktopWeb: () => false }));
  mock.module("../navigation/ResizeHandle", () => ({ ResizeHandle: none, useResizableWidth: () => ({}) }));
  mock.module("../navigation/primaryAppBarGeometry", () => ({ PRIMARY_APP_BAR_HEIGHT: 56 }));
  mock.module("../navigation/primaryAppBarInset", () => ({ usePrimaryAppBarLayout: () => ({}) }));
  mock.module("../../constants/tokens", () => ({
    Typography: {},
    TypeScale: new Proxy({}, { get: () => ({}) }),
  }));
  mock.module("../pets/PetSprite", () => ({ PetSprite: none }));
  mock.module("../icons/Icon", () => ({ Icon: none }));
  mock.module("../ui/IconButton", () => ({ IconButton: host("iconButton") }));
  mock.module("../ui/StatusPill", () => ({ StatusPill: none }));
  mock.module("../terminal/composerMaterial", () => ({ composerNeutralFill: () => "#fff" }));
  mock.module("../calendar/CalendarOriginLink", () => ({ CalendarOriginLink: none }));
  mock.module("./WorkSlip", () => ({
    WORK_STATUS_TONE: {},
    WorkSlip: host("slip"),
    WorkSlipActions: none,
  }));

  const { BrainWorkSheet } = await import("./BrainWorkPanel");

  const slip = (workId: string, group: string) => ({
    workId,
    group,
    status: "waiting",
    statusLabel: "Needs you",
    title: `Work ${workId}`,
    updatedAt: new Date().toISOString(),
  });
  const surface = {
    slips: [slip("a", "needs"), slip("b", "needs"), slip("c", "running")],
    counts: { needs: 2, running: 1, back: 0, waiting: 0 },
  } as any;
  const chrome = { appBackground: "paper", text: "ink", textMuted: "soft", surface: "paper" } as any;
  const sheet = (visible: boolean) => (
    <BrainWorkSheet visible={visible} surface={surface} chrome={chrome} onClose={() => {}} onOpenSlip={() => {}} />
  );

  test("the first commit of an open sheet already holds every slip, and its card is drawn from the first frame", () => {
    // Synchronous on purpose: no timer, frame or effect has run after this commit.
    let renderer!: TestRenderer.ReactTestRenderer;
    act(() => {
      renderer = TestRenderer.create(sheet(false));
    });
    expect(renderer.root.findAllByType("slip" as any)).toHaveLength(0);
    act(() => renderer.update(sheet(true)));
    expect(renderer.root.findAllByType("slip" as any).map((item) => item.props.title)).toEqual(["Work a", "Work b", "Work c"]);
    const frame = renderer.root.findByType("frame" as any);
    expect(frame.props.instantCard).toBe(true);
    // The sheet takes touches at once: the press that opened it has already ended.
    expect(frame.props.settleGuard).toBeFalsy();
  });
}
