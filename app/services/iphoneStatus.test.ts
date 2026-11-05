import { describe, expect, test } from "bun:test";
import { iphoneOffered, iphoneStateWord, type IPhoneDaemonStatus } from "./iphoneStatus";

const ready: IPhoneDaemonStatus = { enabled: true, state: "ready", summary: "ready" };

describe("iPhone use in Labs", () => {
  test("says one word for where it stands", () => {
    expect(iphoneStateWord(null)).toBe("Off");
    expect(iphoneStateWord({ ...ready, enabled: false, state: "off" })).toBe("Off");
    expect(iphoneStateWord(ready)).toBe("Ready");
    expect(iphoneStateWord({ ...ready, active: { worker: "%7", name: "Shopper", since: "" } })).toBe("In use");
    expect(iphoneStateWord({ ...ready, state: "blocked" })).toBe("Set up");
  });

  test("is offered only by a Mewla on a Mac", () => {
    expect(iphoneOffered(null)).toBe(false);
    expect(iphoneOffered({ ...ready, enabled: false, state: "unsupported" })).toBe(false);
    expect(iphoneOffered({ ...ready, enabled: false, state: "off" })).toBe(true);
  });
});
