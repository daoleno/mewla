import { describe, expect, test } from "bun:test";
import { effectiveMinComputerVersion, MIN_COMPUTER_VERSION } from "./version";

describe("effectiveMinComputerVersion", () => {
  test("an app never asks for a computer newer than itself", () => {
    expect(effectiveMinComputerVersion("0.2.88", "0.2.87")).toBe("0.2.87");
    expect(effectiveMinComputerVersion("0.2.88", "0.2.88-beta.2")).toBe("0.2.88-beta.2");
  });

  test("a release app holds the computer to the minimum", () => {
    expect(effectiveMinComputerVersion("0.2.88", "0.2.88")).toBe("0.2.88");
    expect(effectiveMinComputerVersion("0.2.88", "0.3.0")).toBe("0.2.88");
  });

  test("an unreadable app version (a dev build) keeps the minimum", () => {
    expect(effectiveMinComputerVersion("0.2.88", "dev")).toBe("0.2.88");
  });

  test("the minimum is the Expo app's", () => {
    expect(MIN_COMPUTER_VERSION).toBe("0.2.88");
  });
});
