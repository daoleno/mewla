import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import fixture from "../../__fixtures__/telemetry/dashboard-v2.json";
import { normalizeResourceTelemetry } from "../../services/resourceTelemetry";

if (!process.env.ZEN_CONSUMERS_TEST_CHILD) {
  test("Resources consumer interactions on phone and wide layouts", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, ZEN_CONSUMERS_TEST_CHILD: "1" },
    });
    if (result.exitCode !== 0) throw new Error(new TextDecoder().decode(result.stderr));
    expect(result.exitCode).toBe(0);
  });
} else {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  mock.module("react-native", () => ({ View: "View", Text: "Text", ScrollView: "ScrollView", StyleSheet: { create: (v: unknown) => v, hairlineWidth: 1 } }));
  mock.module("react-native-safe-area-context", () => ({ useSafeAreaInsets: () => ({ bottom: 0 }) }));
  mock.module("../ui/EmptyState", () => ({ EmptyState: "EmptyState" }));
  mock.module("react-native-svg", () => ({ default: "Svg", Defs: "Defs", Line: "Line", LinearGradient: "LinearGradient", Path: "Path", Rect: "Rect", Stop: "Stop" }));
  mock.module("../ui/AnimatedPressable", () => ({ AnimatedPressable: "Pressable" }));
  mock.module("../ui/IconButton", () => ({ IconButton: "Pressable" }));
  mock.module("../icons/Icon", () => ({ Icon: "Icon" }));
  mock.module("../ui/StatusMark", () => ({ StatusMark: "StatusMark" }));
  mock.module("../ui/StatusPill", () => ({ StatusPill: "StatusPill" }));
  mock.module("../../constants/tokens", () => ({
    useAppTheme: () => ({ colors: {} }), Radii: {}, TypeScale: {}, UiTextMetrics: {},
  }));
  const { ConsumersSection } = await import("./ResourceConsumersSection");
  const { createResourceStyles } = await import("./resourceStyles");
  const styles = createResourceStyles({} as any);
  const sample = normalizeResourceTelemetry(fixture)!;
  const { ResourcesView } = await import("./ResourcesView");
  test("machine details disclose load, memory, swap, mounts and every PSI window", () => {
    let renderer!: TestRenderer.ReactTestRenderer;
    act(() => { renderer = TestRenderer.create(<ResourcesView telemetry={sample} connected hasServer loading={false} error={null} onRetry={() => {}} />); });
    const content = () => JSON.stringify(renderer.toJSON());
    expect(content()).not.toContain("Cache");
    const toggle = renderer.root.findAllByType("Pressable" as any).find((b) => b.props.accessibilityLabel === "Machine details")!;
    expect(toggle.props.accessibilityState.expanded).toBe(false);
    act(() => toggle.props.onPress());
    expect(content()).toContain("Cache");
    expect(content()).toContain("Shared");
    expect(content()).toContain("Swap");
    expect(content()).toContain("11.42 / 9.82 / 7.16");
    expect(content()).toContain("512 GB");
    expect(content()).toContain("8.4% / 6.72% / 3.36%");
    expect(content()).toContain("0.48% / 0.32% / 0.1%");
    act(() => toggle.props.onPress());
    expect(content()).not.toContain("Cache");
    act(() => renderer.unmount());
  });
  for (const wide of [false, true]) {
    test(`${wide ? "wide" : "phone"}: expands real processes, preserves expansion across polling/sorting, filters owners`, () => {
      let renderer!: TestRenderer.ReactTestRenderer;
      act(() => { renderer = TestRenderer.create(<ConsumersSection telemetry={sample} styles={styles} wide={wide} />); });
      const buttons = () => renderer.root.findAllByType("Pressable" as any);
      const button = (label: string) => buttons().find((node) => node.props.accessibilityLabel?.startsWith(label))!;
      const content = () => JSON.stringify(renderer.toJSON());
      expect(content()).not.toContain("918204");
      act(() => button("Expand Android release build").props.onPress());
      expect(content()).toContain("918204");
      expect(content()).toContain("18420");
      expect(content()).toContain("java");
      expect(button("Collapse Android release build").props.accessibilityState.expanded).toBe(true);
      act(() => button("Sort by CPU").props.onPress());
      const names = buttons().filter((b) => /^(Expand|Collapse) /.test(b.props.accessibilityLabel ?? "")).map((b) => b.props.accessibilityLabel);
      expect(names[1]).toStartWith("Expand node,");
      expect(content()).toContain("918204");
      act(() => renderer.update(<ConsumersSection telemetry={{ ...sample, consumers: sample.consumers.map((c) => ({ ...c, rssBytes: c.rssBytes + 1 })) }} styles={styles} wide={wide} />));
      expect(button("Collapse Android release build").props.accessibilityState.expanded).toBe(true);
      act(() => button("Collapse Android release build").props.onPress());
      expect(content()).not.toContain("918204");
      act(() => button("Filter Orphaned Workers").props.onPress());
      expect(button("Expand Preview browser checks").props.accessibilityLabel).toContain("Orphaned Worker, done");
      expect(button("Expand Android release build")).toBeUndefined();
      expect(content()).toContain("Residual");
      act(() => button("Filter Docker").props.onPress());
      act(() => button("Expand 8f4e6b921d7a").props.onPress());
      expect(content()).toContain("docker:8f4e6b921d7a93bf");
      expect(content()).toContain("postgres");
      act(() => button("Filter All").props.onPress());
      act(() => button("Expand Review API schema").props.onPress());
      expect(content()).toContain("Process details unavailable in this sample.");
      expect(content()).toContain("Unknown process · PID 22001");
      act(() => renderer.unmount());
    });
  }
}
