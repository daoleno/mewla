import { describe, expect, test } from "bun:test";
import { appIdFrom, osMajor, usageDay, usagePlatform, validAppId } from "./usage";

describe("the app's usage count", () => {
  test("names the platform the way the computer's schema does", () => {
    expect(usagePlatform("ios")).toBe("ios");
    expect(usagePlatform("android")).toBe("android");
    expect(usagePlatform("macos")).toBe("mac");
    expect(usagePlatform("web")).toBe("web");
    expect(usagePlatform("windows")).toBe("other");
  });

  test("keeps only the OS's major version", () => {
    expect(osMajor("17.5")).toBe("17");
    expect(osMajor("35")).toBe("35");
    expect(osMajor("15.1.2")).toBe("15");
    expect(osMajor("Mozilla/5.0")).toBe("0");
    expect(osMajor("")).toBe("0");
    expect(osMajor("123")).toBe("0");
  });

  test("an app id is 32 hex from 16 random bytes", () => {
    const id = appIdFrom(new Uint8Array(32).fill(171));
    expect(id).toBe("ab".repeat(16));
    expect(validAppId(id)).toBe(true);
    expect(validAppId("alice")).toBe(false);
    expect(validAppId(null)).toBe(false);
  });

  test("counts days in UTC", () => {
    expect(usageDay(new Date("2030-03-14T23:30:00-08:00"))).toBe("2030-03-15");
  });
});
