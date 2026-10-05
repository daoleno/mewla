import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.ZEN_ENROLLMENT_UI_TEST) {
  test("approval UI in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], { env: { ...process.env, ZEN_ENROLLMENT_UI_TEST: "1" } });
    if (result.exitCode) throw new Error(new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  mock.module("react-native", () => ({ Modal: "Modal", ScrollView: "ScrollView", View: "View", StyleSheet: { create: (s: unknown) => s }, AppState: {} }));
  mock.module("../../constants/tokens", () => ({ useAppTheme: () => ({ colors: {} }) }));
  mock.module("../ui/AppText", () => ({ AppText: "Text" }));
  mock.module("../ui/Button", () => ({ Button: "Button" }));
  mock.module("../../services/enrollmentAPI", () => ({ decideEnrollment: () => {}, decodePendingEnrollment: () => {}, pendingEnrollments: () => {} }));
  mock.module("../../services/websocket", () => ({ wsClient: {} }));
  mock.module("../../store/currentServer", () => ({ useCurrentServer: () => ({}) }));
  const { EnrollmentApprovalSheet } = await import("./EnrollmentApprovalHost");
  const request = { id: "r", deviceName: "Test browser", platform: "web", origin: "https://zen.example", verificationNumber: "042", expiresAt: new Date(Date.now() + 300000).toISOString() };
  test("requires a selection, retains errors for retry, approves and denies", async () => {
    const decisions: Array<[string, boolean]> = [];
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => { renderer = TestRenderer.create(<EnrollmentApprovalSheet request={request} onDecide={async (number, approve) => {
      decisions.push([number, approve]);
      if (approve && number !== "042") throw new Error("That number doesn’t match.");
    }} />); });
    const button = (label: string) => renderer.root.findAllByType("Button" as any).find((node) => node.props.label === label)!;
    expect(button("Approve device").props.disabled).toBe(true);
    const wrong = renderer.root.findAllByType("Button" as any).find((node) => /^\d{3}$/.test(node.props.label) && node.props.label !== "042")!.props.label;
    await act(async () => button(wrong).props.onPress());
    await act(async () => button("Approve device").props.onPress());
    expect(JSON.stringify(renderer.toJSON())).toContain("That number doesn’t match.");
    await act(async () => button("042").props.onPress());
    await act(async () => button("Approve device").props.onPress());
    await act(async () => button("Deny").props.onPress());
    expect(decisions).toEqual([[wrong, true], ["042", true], ["042", false]]);
    await act(async () => renderer.unmount());
  });
}
