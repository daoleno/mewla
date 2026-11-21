import { describe, expect, test } from "bun:test";
import { displayIssue, inputIssue } from "./displayIssue";

describe("displayIssue", () => {
  test("a cancelled request says nothing", () => {
    expect(displayIssue(new Error("Display request cancelled"), "Desk")).toBeNull();
  });

  test("an unreachable computer is a warning naming it", () => {
    expect(displayIssue(new TypeError("Network request failed"), "Desk")).toMatchObject({ title: "Can't reach Desk", tone: "warning" });
    expect(displayIssue(new Error("Desk did not answer"), "Desk")).toMatchObject({ title: "Can't reach Desk" });
  });

  test("a lost viewer keeps the window", () => {
    expect(displayIssue(new Error("viewer closed"), "Desk")).toMatchObject({ title: "Lost connection to the browser", detail: "Still open on Desk." });
  });

  test("anything else did not finish, with the raw words kept for diagnostics", () => {
    const issue = displayIssue(new Error("This server does not run agent displays."), "Desk")!;
    expect(issue.title).toBe("That didn't finish");
    expect(issue.tone).toBe("danger");
    expect(issue.diagnostic).toBe("This server does not run agent displays.");
  });
});

describe("inputIssue", () => {
  test("input that reaches the page says nothing", () => {
    expect(inputIssue("", "Mac")).toBeNull();
  });
  test("each problem names its one fix", () => {
    expect(inputIssue("the Mewla extension in Chrome is too old to take input from Screen", "Mac")?.detail).toBe("Reload Mewla's extension in Chrome on Mac.");
    expect(inputIssue("the Mewla extension isn't connected: Chrome isn't running with it", "Mac")?.detail).toBe("Open Chrome on Mac.");
    const page = inputIssue("the page in front is not one the agent's browser tools hold", "Mac");
    expect(page?.title).toBe("This page isn't taking your input");
    expect(page?.detail).toBe("Ask the agent to open this page again.");
    expect(page?.diagnostic).toBe("the page in front is not one the agent's browser tools hold");
  });
});
