import { describe, expect, test } from "bun:test";
import { terminalFallback } from "./terminalFallback";

describe("the terminal's offline card", () => {
  test("is absent while connected", () => {
    expect(terminalFallback("connected", "")).toBeNull();
  });

  test("says it is reconnecting, busy", () => {
    expect(terminalFallback("connecting", "")).toMatchObject({ busy: true, title: "Reconnecting to daemon" });
  });

  test("names the connection's problem when it is waiting", () => {
    expect(terminalFallback("waiting", "Device not paired")).toMatchObject({ busy: false, title: "Daemon unavailable", detail: "Device not paired" });
    expect(terminalFallback("idle", "")!.detail).toBe("Start mewla on that machine, or bring the network or tunnel back.");
  });
});
