import { expect, mock, test } from "bun:test";
import React, { createContext, useContext, useEffect } from "react";
import TestRenderer, { act } from "react-test-renderer";

// Keep the React Native mocks out of other tests' module caches.
if (!process.env.MEWLA_PRIMARY_SCENE_TEST_CHILD) {
  test("primary tab visibility follows focus without remounting its content", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_PRIMARY_SCENE_TEST_CHILD: "1" },
    });
    if (result.exitCode) {
      throw new Error(new TextDecoder().decode(result.stdout) + new TextDecoder().decode(result.stderr));
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const Focus = createContext(false);
  const Desktop = createContext(true);
  mock.module("expo-router", () => ({ useIsFocused: () => useContext(Focus) }));
  mock.module("./useDesktopWeb", () => ({ useDesktopWeb: () => useContext(Desktop) }));
  mock.module("react-native", () => ({
    View: ({ children, ...props }: any) => React.createElement("view", props, children),
    StyleSheet: { create: (styles: unknown) => styles },
  }));
  const { PrimaryTabScene } = await import("./PrimaryTabScene");

  test("both switch directions update visibility and retain page state", () => {
    let mounts = 0;
    function Page() {
      useEffect(() => { mounts++; }, []);
      return React.createElement("page");
    }
    const page = <PrimaryTabScene><Page /></PrimaryTabScene>;
    const tree = (focused: boolean, desktop = true) => (
      <Desktop.Provider value={desktop}>
        <Focus.Provider value={focused}>{page}</Focus.Provider>
      </Desktop.Provider>
    );
    let renderer!: TestRenderer.ReactTestRenderer;
    act(() => { renderer = TestRenderer.create(tree(false)); });
    const hidden = () => renderer.root.findByType("view" as any).props.style
      .some((style: { visibility?: string } | null) => style?.visibility === "hidden");
    expect(hidden()).toBe(true);
    for (const focused of [true, false, true, false, true]) {
      act(() => { renderer.update(tree(focused)); });
      expect(hidden()).toBe(!focused);
      expect(mounts).toBe(1);
    }
    // Native and narrow web retain the swipe pager, including its other page.
    act(() => { renderer.update(tree(false, false)); });
    expect(hidden()).toBe(false);
    expect(mounts).toBe(1);
    act(() => { renderer.unmount(); });
  });
}
