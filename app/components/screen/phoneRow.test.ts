import { describe, expect, test } from "bun:test";
import { phoneMenuItem } from "./phoneRow";

describe("phoneMenuItem", () => {
  const open = () => {};

  test("is absent where phone use is unavailable or off", () => {
    expect(phoneMenuItem({ available: false, enabled: true, status: null }, open)).toBeNull();
    expect(phoneMenuItem({ available: true, enabled: false, status: null }, open)).toBeNull();
  });

  test("says who is using the phone, or that it is ready", () => {
    expect(phoneMenuItem({ available: true, enabled: true, status: { agent: "Ada" } }, open)?.detail).toBe("Ada");
    expect(phoneMenuItem({ available: true, enabled: true, status: null }, open)?.detail).toBe("Ready");
  });

  test("opens the phone's own page", () => {
    let opened = 0;
    phoneMenuItem({ available: true, enabled: true, status: null }, () => { opened += 1; })?.onPress();
    expect(opened).toBe(1);
  });
});
