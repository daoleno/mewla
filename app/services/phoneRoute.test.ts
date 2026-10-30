import { describe, expect, test } from "bun:test";
import { redirectPhonePath } from "./phoneRoute";

describe("redirectPhonePath", () => {
  test("the notification link opens the Phone page", () => {
    expect(redirectPhonePath("mewla://phone")).toBe("/phone");
    expect(redirectPhonePath("mewla://phone/")).toBe("/phone");
  });
  test("other links are left alone", () => {
    expect(redirectPhonePath("mewla://plugins?state=x")).toBe("mewla://plugins?state=x");
    expect(redirectPhonePath("mewla://live/open")).toBe("mewla://live/open");
    expect(redirectPhonePath("/phone")).toBe("/phone");
    expect(redirectPhonePath("/list")).toBe("/list");
  });
});
