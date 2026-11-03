import { describe, expect, test } from "bun:test";
import { executorAccountRows } from "./executorAccountRows";

describe("executorAccountRows", () => {
  test("one row per installed agent, with the account when known", () => {
    const rows = executorAccountRows(
      [
        { id: "claude", name: "claude" },
        { id: "claude-work", name: "claude-work" },
        { id: "codex", name: "codex" },
        { id: "grok", name: "grok" },
      ],
      {
        claude: { installed: true, signed_in: true },
        "claude-work": { installed: true, signed_in: true, account: "me@work.example" },
        codex: { installed: true, signed_in: false },
      },
    );
    expect(rows.map((row) => [row.id, row.value])).toEqual([
      ["claude", "Signed in"],
      ["claude-work", "me@work.example"],
      ["codex", "Not signed in"],
    ]);
  });
});
