import { describe, expect, test } from "bun:test";
import type { Transport } from "../transport";
import { createSession } from "./create";

const refusing = (code: string, message: string) =>
  ({ state: "connected", request: async () => { throw Object.assign(new Error(message), { code }); } }) as unknown as Transport;

describe("a launch the computer refuses", () => {
  test("because the agent is not installed is a failure with its cause, not an unknown outcome", async () => {
    const result = await createSession(refusing("agent_not_installed", "Antigravity (agy) is not installed on this computer."), { command: "agy" });
    expect(result).toEqual({ kind: "failed", message: "Antigravity (agy) is not installed on this computer.", requiresReconcileBeforeCreate: false });
  });

  test("for any other reason stays unknown until the list shows what happened", async () => {
    const result = await createSession(refusing("create_session_failed", "boom"), { command: "claude" });
    expect(result.kind).toBe("ambiguous");
  });
});
