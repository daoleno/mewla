import { describe, expect, test } from "bun:test";
import { agentKindFromProvider, presentWorker } from "./workerPresentation";

const base = { name: "", summary: "", last_output_lines: [] as string[] };

describe("agentKindFromProvider", () => {
  test("presents a Worker by the provider the daemon names", () => {
    for (const provider of ["claude", "codex", "cursor", "grok", "pi", "opencode", "dsh"]) {
      expect(agentKindFromProvider(provider)).toBe(provider as never);
    }
  });

  test("a Worker with no provider, or one the app has no art for, is a terminal", () => {
    expect(agentKindFromProvider(undefined)).toBe("terminal");
    expect(agentKindFromProvider("")).toBe("terminal");
    expect(agentKindFromProvider("custom")).toBe("terminal");
  });

  test("the command line is never read to pick the agent", () => {
    expect(presentWorker({ ...base, command: "codex --yolo" }).kind).toBe("terminal");
    expect(presentWorker({ ...base, command: "my-proxy", provider: "codex" }).kind).toBe("codex");
  });
});
