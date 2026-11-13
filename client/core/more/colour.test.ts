import { describe, expect, test } from "bun:test";
import { BRAND_MARKS, brandMark, DEFAULT_METER_THRESHOLDS, meterTone, rowLeading } from "./colour";
import { telegramLabel, telegramLabelTone, type TelegramConnectionStatus } from "../settings/telegram";

describe("a row's leading icon (SPEC §10.4 row tiles)", () => {
  test("a named product gets its real mark, however it is spelled", () => {
    expect(brandMark("Telegram")).toBe("telegram");
    expect(brandMark("GitHub")).toBe("github");
    expect(brandMark("Node.js")).toBe("nodejs");
    expect(brandMark("node")).toBe("nodejs");
    expect(brandMark("go")).toBe("go");
    expect(brandMark("dockerd")).toBe("docker");
    expect(brandMark("python3")).toBe("python");
    expect(brandMark("postgres")).toBe("postgresql");
    expect(brandMark(" Docker ")).toBe("docker");
  });

  test("a generic row keeps its group's tile, never a guessed mark", () => {
    expect(brandMark("bash")).toBeNull();
    expect(brandMark("")).toBeNull();
    expect(brandMark(undefined)).toBeNull();
    expect(rowLeading("bash", { hue: "teal", glyph: "cube" })).toEqual({ kind: "tile", hue: "teal", glyph: "cube" });
    expect(rowLeading(null, { hue: "violet", glyph: "puzzle" })).toEqual({ kind: "tile", hue: "violet", glyph: "puzzle" });
  });

  test("a named product's row is its mark, not the tile", () => {
    expect(rowLeading("Linear", { hue: "violet", glyph: "puzzle" })).toEqual({ kind: "mark", mark: "linear" });
  });

  test("every mark has a name that leads to it", () => {
    for (const mark of BRAND_MARKS) expect(brandMark(mark)).toBe(mark);
  });
});

describe("a meter's fill (SPEC §10.4 data)", () => {
  test("its own colour under the warning threshold, warn over it, red over the critical one", () => {
    expect(meterTone(0.5)).toBe("fill");
    expect(meterTone(DEFAULT_METER_THRESHOLDS.warn)).toBe("warn");
    expect(meterTone(0.94)).toBe("warn");
    expect(meterTone(0.95)).toBe("error");
    expect(meterTone(1)).toBe("error");
  });

  test("a metric's own thresholds win over the defaults", () => {
    expect(meterTone(0.7, { warn: 0.6, critical: 0.9 })).toBe("warn");
    expect(meterTone(0.85, { warn: 0.9, critical: 0.97 })).toBe("fill");
  });
});

describe("Telegram's state (SPEC §10.4 state)", () => {
  const status = (over: Partial<TelegramConnectionStatus>): TelegramConnectionStatus => ({ state: "connected", enabled: true, bot_username: "mewla_bot", owner_hint: "@me", binding_pending: false, ...over });

  test("connected is ok, waiting for its owner or degraded is warn, nothing set up is off", () => {
    expect(telegramLabelTone(telegramLabel(status({}), true, false))).toBe("ok");
    expect(telegramLabelTone(telegramLabel(status({ owner_hint: undefined, state: "setup_pending" }), true, false))).toBe("warn");
    expect(telegramLabelTone(telegramLabel(status({ state: "degraded" }), true, false))).toBe("warn");
    expect(telegramLabelTone(telegramLabel(status({ enabled: false, state: "disabled" }), true, false))).toBe("off");
    expect(telegramLabelTone(telegramLabel(status({ bot_username: undefined }), true, false))).toBe("off");
  });

  test("its computer away is warn, never red: nothing that needs you stops", () => {
    expect(telegramLabelTone(telegramLabel(status({}), false, false))).toBe("warn");
  });
});
