import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

// react-native and the section's leaf components are mocked: keep that out of the
// shared Bun process.
if (!process.env.MEWLA_ABOUT_TEST_CHILD) {
  test("About shows the TestFlight update row on iOS, in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_ABOUT_TEST_CHILD: "1" },
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
    Platform: { OS: "ios" },
    StyleSheet: { create: (value: unknown) => value },
    View: host("view"),
  }));
  mock.module("expo-router", () => ({ useRouter: () => ({ push() {} }) }));
  mock.module("../../constants/tokens", () => ({
    useAppTheme: () => ({ colors: { surfaceSubtle: "gray", accent: "blue" } }),
  }));
  mock.module("../../constants/appVersion", () => ({ appVersion: "0.2.87" }));
  mock.module("../../services/aboutPages", () => ({
    PRIVACY_POLICY_URL: "",
    SUPPORT_URL: "",
    openAboutPage: async () => {},
  }));
  mock.module("../../services/useAppUpdate", () => ({ useAppUpdate: () => ({}) }));
  mock.module("../ui/Button", () => ({ Button: host("button") }));
  mock.module("../ui", () => ({
    ListRow: (props: any) => React.createElement("row", props, props.trailing),
    ListSection: host("section"),
  }));
  mock.module("../mewla/MewlaMark", () => ({ MewlaMark: host("mark") }));

  const { AboutRows } = await import("./AboutSection");

  const render = async (state: any, press = () => {}) => {
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(<AboutRows state={state} press={press} />);
    });
    return renderer;
  };

  test("a newer TestFlight build gives the same Update row as Android, and its button presses", async () => {
    let pressed = 0;
    const update = { version: "0.2.88", buildNumber: 130, appId: "6790486999" };
    const renderer = await render({ kind: "testflightAvailable", update }, () => (pressed += 1));
    const row = renderer.root.findAllByType("row" as any).find((item) => item.props.title === "Update available")!;
    expect(row.props.subtitle).toBe("v0.2.88");
    expect(row.props.accessibilityLabel).toBe("Update available, v0.2.88, Update");
    const button = row.findByType("button" as any);
    expect(button.props.label).toBe("Update");
    expect(button.props.variant).toBe("filled");
    act(() => button.props.onPress());
    expect(pressed).toBe(1);
  });

  test("a TestFlight build with nothing newer says it is up to date; an untracked build shows no row", async () => {
    const current = await render({ kind: "current" });
    expect(current.root.findAllByType("row" as any).map((r) => r.props.title)).toContain("Check for updates");
    const hidden = await render({ kind: "hidden" });
    const titles = hidden.root.findAllByType("row" as any).map((r) => r.props.title);
    expect(titles).not.toContain("Check for updates");
    expect(titles).not.toContain("Update available");
  });
}
