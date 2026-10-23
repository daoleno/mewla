import { describe, expect, it } from "bun:test";
import { shouldAskForPushPermission } from "./pushPermissionPolicy";

describe("shouldAskForPushPermission", () => {
  it("asks a person who has never been asked", () => {
    expect(
      shouldAskForPushPermission({ granted: false, canAskAgain: true }, false),
    ).toBe(true);
  });

  it("never repeats the prompt after Mewla asked once", () => {
    expect(
      shouldAskForPushPermission({ granted: false, canAskAgain: true }, true),
    ).toBe(false);
  });

  it("does not ask when the system will not show the prompt or it is granted", () => {
    expect(
      shouldAskForPushPermission({ granted: false, canAskAgain: false }, false),
    ).toBe(false);
    expect(
      shouldAskForPushPermission({ granted: true, canAskAgain: true }, false),
    ).toBe(false);
  });
});
