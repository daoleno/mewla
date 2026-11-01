import { describe, expect, test } from "bun:test";
import { importFailureToast, importProgressToast } from "./importFeedback";
import { PairingCancelledError } from "./pairingScope";

describe("importing a pairing link opened from outside the app", () => {
  test("a failure says why instead of being silent", () => {
    expect(importFailureToast(new Error("This pairing code was already used or has expired."))).toEqual({
      title: "Couldn't pair this computer",
      detail: "This pairing code was already used or has expired.",
      tone: "error",
    });
    expect(importFailureToast("boom")?.detail).toBe("boom");
    expect(importFailureToast(undefined)?.detail).toBeUndefined();
  });

  test("cancelling the confirmation is not a failure", () => {
    expect(importFailureToast(new PairingCancelledError())).toBeNull();
  });

  test("progress is shown as information", () => {
    expect(importProgressToast("Connecting to DUN MBP through Mewla…")).toEqual({
      title: "Connecting to DUN MBP through Mewla…",
      tone: "info",
    });
  });
});
