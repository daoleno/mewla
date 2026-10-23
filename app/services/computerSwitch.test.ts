import { describe, expect, test } from "bun:test";
import { switchToComputer } from "./computerSwitch";

describe("switchToComputer", () => {
  test("lets the computer connect, then makes it current", async () => {
    const calls: string[] = [];
    await switchToComputer(
      "b",
      async (id) => void calls.push(`switch:${id}`),
      async (id, enabled) => void calls.push(`connect:${id}:${enabled}`),
    );
    expect(calls).toEqual(["connect:b:true", "switch:b"]);
  });

  test("does not switch when the computer can't be allowed to connect", async () => {
    const calls: string[] = [];
    await expect(
      switchToComputer(
        "b",
        async (id) => void calls.push(`switch:${id}`),
        async () => {
          throw new Error("storage failed");
        },
      ),
    ).rejects.toThrow("storage failed");
    expect(calls).toEqual([]);
  });
});
