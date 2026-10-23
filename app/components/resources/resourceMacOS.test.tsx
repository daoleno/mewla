import { expect, mock, test } from "bun:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import macos from "../../__fixtures__/telemetry/macos-v2.json";
import { hasMachineReading, memoryPressureSeries, normalizeResourceTelemetry } from "../../services/resourceTelemetry";

// A Mac reports memory pressure, not PSI, and its Resources screen must say
// so. The daemon's first Mac release sent nothing at all; that must not read
// as "Normal".

test("a Mac payload keeps the kernel's pressure level apart from PSI", () => {
  const sample = normalizeResourceTelemetry(macos)!;
  expect(sample.memoryPressure).toEqual({ level: "normal", freePercent: 38 });
  expect(sample.psi).toEqual({ cpu: undefined, memory: undefined, io: undefined });
  expect(sample.unavailable).toEqual(["psi", "memory.shared"]);
  expect(sample.cpu.perCorePercent).toHaveLength(10);
  expect(sample.history.filter((point) => point.memoryPressure === "warn")).toHaveLength(8);
  const steps = memoryPressureSeries(sample).map((point) => point.value);
  expect(new Set(steps)).toEqual(new Set([0, 0.5]));
  expect(hasMachineReading(sample)).toBe(true);
});

test("a level the app does not know is dropped, not shown as normal", () => {
  const sample = normalizeResourceTelemetry({ sampled_at: "2026-10-01T06:00:00Z", memory_pressure: { level: "urgent" } })!;
  expect(sample.memoryPressure).toBeUndefined();
  expect(hasMachineReading(sample)).toBe(false);
});

test("a machine reading means any core measurement, so Linux's first sample still counts", () => {
  expect(hasMachineReading(normalizeResourceTelemetry({ sampled_at: "2026-10-01T06:00:00Z", state: "normal" })!)).toBe(false);
  expect(hasMachineReading(normalizeResourceTelemetry({
    sampled_at: "2026-10-01T06:00:00Z", memory: { total_bytes: 1000, available_bytes: 500 }, cpu: { load1: 0.2 },
  })!)).toBe(true);
  expect(hasMachineReading(normalizeResourceTelemetry({
    sampled_at: "2026-10-01T06:00:00Z", psi: { cpu: { some: { avg10: 0, avg60: 0, avg300: 0 } } },
  })!)).toBe(true);
});

if (!process.env.MEWLA_MACOS_RENDER_CHILD) {
  test("Resources screen on a Mac, with and without a reading", () => {
    const result = Bun.spawnSync([process.execPath, "test", import.meta.filename], {
      env: { ...process.env, MEWLA_MACOS_RENDER_CHILD: "1" },
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
    useAppTheme: () => ({ colors: { statusUnknown: "grey-ring" } }), Radii: {}, TypeScale: {}, UiTextMetrics: {},
  }));
  const { ResourcesView } = await import("./ResourcesView");
  const render = (telemetry: ReturnType<typeof normalizeResourceTelemetry>) => {
    let renderer!: TestRenderer.ReactTestRenderer;
    act(() => { renderer = TestRenderer.create(<ResourcesView telemetry={telemetry} connected hasServer loading={false} error={null} onRetry={() => {}} />); });
    return renderer;
  };
  const text = (renderer: TestRenderer.ReactTestRenderer) => JSON.stringify(renderer.toJSON());
  const headlineMark = (renderer: TestRenderer.ReactTestRenderer) => renderer.root.findAllByType("StatusMark" as any)[0].props;

  test("no reading shows a neutral ring, never Normal", () => {
    const empty = render(normalizeResourceTelemetry({ version: 2, sampled_at: "2026-10-23T09:00:00Z", state: "normal" }));
    expect(headlineMark(empty)).toMatchObject({ status: "blocked", color: "grey-ring" });
    expect(text(empty)).not.toContain("Normal");
    act(() => empty.unmount());
  });

  test("a Mac with readings shows its state, memory pressure and no PSI or fake Shared", () => {
    const mac = render(normalizeResourceTelemetry(macos));
    expect(headlineMark(mac).status).toBe("ready");
    const content = text(mac);
    expect(content).toContain("Memory pressure");
    expect(content).toContain("38% free");
    expect(content).toContain("Load 3.1 · 10 cores");
    expect(content).not.toContain("PSI");
    const toggle = mac.root.findAllByType("Pressable" as any).find((b) => b.props.accessibilityLabel === "Machine details")!;
    act(() => toggle.props.onPress());
    expect(text(mac)).toContain("Cache");
    expect(text(mac)).not.toContain("Shared");
    act(() => mac.unmount());
  });

  test("a warning level reads as Warning on the tile", () => {
    const sample = normalizeResourceTelemetry({ ...macos, memory_pressure: { level: "warn", free_percent: 9 } });
    const warn = render(sample);
    expect(text(warn)).toContain("Warning");
    expect(text(warn)).toContain("9% free");
    act(() => warn.unmount());
  });
}
