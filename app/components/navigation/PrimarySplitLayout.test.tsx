import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";

if (!process.env.MEWLA_SPLIT_LAYOUT_TEST_CHILD) {
  test("split layout keeps the page across a fold, in isolation", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_SPLIT_LAYOUT_TEST_CHILD: "1" },
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
  mock.module("react-native", () => ({
    StyleSheet: { create: (value: unknown) => value },
    View: host("view"),
  }));

  const { PrimarySplitLayout } = await import("./PrimarySplitLayout");

  const events: string[] = [];
  function Page() {
    const [draft] = React.useState(() => `draft-${events.length}`);
    React.useEffect(() => {
      events.push("mount");
      return () => {
        events.push("unmount");
      };
    }, []);
    return React.createElement("page", { draft });
  }
  const sidebar = React.createElement("sidebar");
  const render = (docked: boolean) => (
    <PrimarySplitLayout sidebar={docked ? sidebar : null}>
      <Page />
    </PrimarySplitLayout>
  );

  test("docking and undocking never remounts the page", async () => {
    events.length = 0;
    let renderer!: TestRenderer.ReactTestRenderer;
    await act(async () => {
      renderer = TestRenderer.create(render(false));
    });
    const page = renderer.root.findByType("page" as any);
    expect(renderer.root.findAllByType("sidebar" as any)).toHaveLength(0);

    await act(async () => renderer.update(render(true)));
    expect(renderer.root.findAllByType("sidebar" as any)).toHaveLength(1);
    expect(renderer.root.findByType("page" as any)).toBe(page);

    await act(async () => renderer.update(render(false)));
    expect(renderer.root.findAllByType("sidebar" as any)).toHaveLength(0);
    expect(renderer.root.findByType("page" as any)).toBe(page);
    expect(events).toEqual(["mount"]);
  });
}
