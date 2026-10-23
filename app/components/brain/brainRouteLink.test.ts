import { describe, expect, test } from "bun:test";
import { brainRouteLink } from "./brainRouteLink";

const studio = { serverId: "studio", brainThreadId: "t1", brainMessageId: "m1" };

describe("brain route link", () => {
  test("a link to another computer switches once, then is remembered", () => {
    const first = brainRouteLink(studio, "homelab", "");
    expect(first).toMatchObject({ switchTo: "studio", showsCurrent: false, threadId: undefined, shown: "" });

    const arrived = brainRouteLink(studio, "studio", "");
    expect(arrived).toMatchObject({ switchTo: "studio", showsCurrent: true, threadId: "t1", shown: "studio|t1|m1" });
  });

  test("switching computer afterwards is not undone and does not carry the thread over", () => {
    const afterSwitch = brainRouteLink(studio, "homelab", "studio|t1|m1");
    expect(afterSwitch.switchTo).toBe("");
    expect(afterSwitch.showsCurrent).toBe(true);
    expect(afterSwitch.threadId).toBeUndefined();
  });

  test("a new notification for the same computer switches again", () => {
    const next = brainRouteLink({ ...studio, brainMessageId: "m2" }, "homelab", "studio|t1|m1");
    expect(next).toMatchObject({ switchTo: "studio", showsCurrent: false, threadId: undefined });
  });

  test("no link leaves the current computer and any thread alone", () => {
    expect(brainRouteLink({}, "homelab", "")).toMatchObject({ switchTo: "", showsCurrent: true, threadId: undefined, shown: "" });
    expect(brainRouteLink({ brainThreadId: "t9" }, "homelab", "").threadId).toBe("t9");
  });
});
