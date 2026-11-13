import { describe, expect, test } from "bun:test";
import { consumerLead, meterState, meterWord, pressureTone, programName, resourceMeterTone } from "./colour";
import type { ResourceConsumer, ResourceSignal, ResourceTelemetry } from "./resourceTelemetry";

const telemetry = (signals: ResourceSignal[], extra: Partial<ResourceTelemetry> = {}): ResourceTelemetry => ({
  version: 1, sampledAt: 0, state: "normal", cpu: { perCorePercent: [] }, memory: {}, psi: {}, unavailable: [], disks: [], consumers: [], history: [], signals, ...extra,
});
const signal = (name: string, state: ResourceSignal["state"]): ResourceSignal => ({ name, value: 1, threshold: 1, state });

describe("Resources' state (SPEC §10.4)", () => {
  test("the page's dot follows the daemon's pressure", () => {
    expect(pressureTone("normal")).toBe("ok");
    expect(pressureTone("elevated")).toBe("warn");
    expect(pressureTone("critical")).toBe("error");
  });

  test("a tile turns on the daemon's thresholds, not on its share", () => {
    const t = telemetry([signal("memory.available_percent", "elevated"), signal("psi.cpu.some.avg10", "critical")]);
    expect(resourceMeterTone(t, "memory", 0.4)).toBe("warn");
    expect(resourceMeterTone(t, "cpu", 0.3)).toBe("error");
    // 97 % used memory the daemon calls fine stays the meter's colour: the client does not judge it again.
    expect(resourceMeterTone(telemetry([]), "memory", 0.97)).toBe("fill");
  });

  test("the worst of a meter's signals wins; macOS's own memory pressure counts", () => {
    const t = telemetry([signal("psi.memory.some.avg60", "elevated"), signal("psi.memory.full.avg60", "critical")]);
    expect(meterState(t, "memory")).toBe("critical");
    expect(meterState(telemetry([], { memoryPressure: { level: "warn" } as ResourceTelemetry["memoryPressure"] }), "memory")).toBe("elevated");
  });

  test("a disk is judged by its own mount", () => {
    const t = telemetry([signal("disk.free_percent:/data", "critical")]);
    expect(resourceMeterTone(t, "disk", 0.5, "/data")).toBe("error");
    expect(resourceMeterTone(t, "disk", 0.5, "/")).toBe("fill");
  });

  test("a metric the daemon has no threshold for uses 80 % and 95 %", () => {
    expect(resourceMeterTone(telemetry([]), null, 0.5)).toBe("fill");
    expect(resourceMeterTone(telemetry([]), null, 0.85)).toBe("warn");
    expect(resourceMeterTone(telemetry([]), null, 0.96)).toBe("error");
  });

  test("the status word beside the number", () => {
    expect(meterWord("fill")).toBeNull();
    expect(meterWord("warn")).toBe("High");
    expect(meterWord("error")).toBe("Critical");
  });
});

describe("Using this machine: each row's mark (SPEC §10.4)", () => {
  const consumer = (over: Partial<ResourceConsumer>): ResourceConsumer => ({ commands: [], processes: [], owner: "user", rssBytes: 0, processCount: 1, kinds: [], ...over });

  test("a known program gets its mark, from its command's first word without its path", () => {
    expect(consumerLead(consumer({ commands: ["/usr/bin/node server.js"] }))).toEqual({ kind: "mark", mark: "nodejs" });
    expect(consumerLead(consumer({ commands: ["go test ./..."] }))).toEqual({ kind: "mark", mark: "go" });
    expect(consumerLead(consumer({ processes: [{ command: "python3 -m http.server" }] }))).toEqual({ kind: "mark", mark: "python" });
  });

  test("an unknown program is the teal cube tile", () => {
    expect(consumerLead(consumer({ commands: ["bun run dev"] }))).toEqual({ kind: "tile", hue: "teal", glyph: "cube" });
    expect(consumerLead(consumer({}))).toEqual({ kind: "tile", hue: "teal", glyph: "cube" });
  });

  test("Workers and Brain show their agent, Docker its whale, the daemon Mewla's own", () => {
    expect(consumerLead(consumer({ owner: "worker", executor: "claude", commands: ["node"] }))).toEqual({ kind: "agent", agent: "claude" });
    expect(consumerLead(consumer({ owner: "orphaned_worker", executor: "codex" }))).toEqual({ kind: "agent", agent: "codex" });
    expect(consumerLead(consumer({ owner: "brain" }))).toEqual({ kind: "mewla" });
    expect(consumerLead(consumer({ owner: "docker", commands: ["postgres"] }))).toEqual({ kind: "mark", mark: "docker" });
    expect(consumerLead(consumer({ commands: ["/home/me/.local/bin/mewla daemon"] }))).toEqual({ kind: "mewla" });
  });

  test("a program's name", () => {
    expect(programName("C:\\tools\\node.exe app.js")).toBe("node.exe");
    expect(consumerLead(consumer({ commands: ["C:\\tools\\node.exe app.js"] }))).toEqual({ kind: "mark", mark: "nodejs" });
    expect(programName("   ")).toBeUndefined();
  });
});
