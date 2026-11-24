import { describe, expect, test } from "nsh/test";
import { brainComposerPlaceholder } from "./Home";

describe("Brain's composer words (owner 2026-11-24, a Windows user's reload)", () => {
  test("a page still opening its socket says Connecting…, never Sends when back online; away after 10 s says it", () => {
    expect(brainComposerPlaceholder({ waking: false, connection: "idle", shown: "idle" })).toBe("Connecting…");
    expect(brainComposerPlaceholder({ waking: false, connection: "connecting", shown: "connecting" })).toBe("Connecting…");
    expect(brainComposerPlaceholder({ waking: false, connection: "connecting", shown: "waiting" })).toBe("Sends when back online");
    expect(brainComposerPlaceholder({ waking: false, connection: "connected", shown: "connected" })).toBe("Tell Brain");
    expect(brainComposerPlaceholder({ waking: true, connection: "connected", shown: "connected" })).toBe("Connecting…");
  });
});
