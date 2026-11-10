import { describe, expect, test } from "bun:test";
import { screenFailureText } from "./screenFailure";

describe("screenFailureText", () => {
  test("keeps a plain-text reason", () => {
    expect(screenFailureText(500, "  screen helper exited\n")).toBe("screen helper exited");
  });

  test("never shows an HTML page", () => {
    expect(screenFailureText(502, "<!doctype html>\n<html><head>")).toBe("Can't reach this computer.");
    expect(screenFailureText(500, "<html>oops</html>")).toBe("Screen request failed");
  });

  test("falls back when the body is empty", () => {
    expect(screenFailureText(504, "")).toBe("Can't reach this computer.");
  });
});
