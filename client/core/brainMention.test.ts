import { describe, expect, test } from "bun:test";
import { activeExecutorMentionAtEnd, executorMentionChoices, insertExecutorMention } from "./brainMention";

const claude = { id: "claude", name: "claude", provider: "claude" };
const codex = { id: "codex", name: "codex", provider: "codex" };
const agent = { id: "agent", name: "", provider: "cursor" };

describe("Brain's @ agent picker (the Expo app's BrainExecutorMentionPicker)", () => {
  test("reads the @word being typed at the end of the draft", () => {
    expect(activeExecutorMentionAtEnd("@")).toEqual({ start: 0, query: "" });
    expect(activeExecutorMentionAtEnd("Ask @co")).toEqual({ start: 4, query: "co" });
    // An email, a finished mention, or none.
    expect(activeExecutorMentionAtEnd("me@codex")).toBeNull();
    expect(activeExecutorMentionAtEnd("Ask @codex ")).toBeNull();
    expect(activeExecutorMentionAtEnd("Ask codex")).toBeNull();
  });

  test("offers the agents whose id, name or provider starts with the query", () => {
    expect(executorMentionChoices([claude, codex, agent], "").map((e) => e.id)).toEqual(["claude", "codex", "agent"]);
    expect(executorMentionChoices([claude, codex, agent], "co").map((e) => e.id)).toEqual(["codex"]);
    // Cursor Agent, by its provider's name.
    expect(executorMentionChoices([claude, codex, agent], "cur").map((e) => e.id)).toEqual(["agent"]);
  });

  test("a pick replaces the @word with the handle and a space", () => {
    expect(insertExecutorMention("Ask @co", { start: 4, query: "co" }, "codex")).toBe("Ask @codex ");
  });
});
