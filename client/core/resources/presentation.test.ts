import { describe, expect, test } from "bun:test";
import {
  consumerAccessibilityLabel,
  consumerCommands,
  consumerCopyChoices,
  consumerRowKeys,
  consumerRowMeta,
  consumersWide,
  consumerStatus,
  memoryPressureMeterLabel,
  memoryPressureMicro,
  memoryUsedBytes,
  processCopyChoices,
  processesByMemory,
  psiChartLabel,
  psiPeak,
  psiResourceChartLabel,
  tileBasis,
  usesMemoryPressure,
} from "./presentation";
import { normalizeResourceTelemetry, type ResourceConsumer } from "./resourceTelemetry";

const telemetry = (extra: Record<string, unknown>) => normalizeResourceTelemetry({ version: 2, sampled_at: "2026-10-01T06:00:00Z", ...extra })!;

const consumer = (over: Partial<ResourceConsumer>): ResourceConsumer => ({ commands: [], processes: [], owner: "worker", rssBytes: 1536 * 1024 * 1024, cpuPercent: 12, processCount: 3, kinds: [], ...over });

describe("layout", () => {
  test("tiles go four across on wide screens, one column on the narrowest, else two", () => {
    expect(tileBasis(1200)).toBe("23%");
    expect(tileBasis(1040)).toBe("23%");
    expect(tileBasis(390)).toBe("48%");
    expect(tileBasis(320)).toBe("100%");
    expect(consumersWide(760)).toBe(true);
    expect(consumersWide(759)).toBe(false);
  });
});

describe("memory and pressure", () => {
  test("memory in use is total less available when both are known, as the percentage is", () => {
    expect(memoryUsedBytes(telemetry({ memory: { total_bytes: 1000, available_bytes: 400, used_bytes: 300 } }))).toBe(600);
    expect(memoryUsedBytes(telemetry({ memory: { total_bytes: 1000, used_bytes: 300 } }))).toBe(300);
    expect(memoryUsedBytes(telemetry({}))).toBeUndefined();
  });

  test("the PSI peak is the highest some 10 s average, the first on a tie; none without PSI", () => {
    const t = telemetry({ psi: { cpu: { some: { avg10: 2, avg60: 0, avg300: 0 } }, memory: { some: { avg10: 5, avg60: 0, avg300: 0 } }, io: { some: { avg10: 5, avg60: 0, avg300: 0 } } } });
    expect(psiPeak(t)).toEqual({ key: "memory", value: 5 });
    expect(psiPeak(telemetry({}))).toBeUndefined();
    expect(psiChartLabel("memory")).toBe("PSI Mem some history, scale 0–100%");
    expect(psiChartLabel(undefined)).toBe("PSI unavailable some history, scale 0–100%");
    expect(psiResourceChartLabel("io")).toBe("I/O PSI some history, scale 0–100%");
  });

  test("a Mac reports a memory pressure level: the meter says it, the micro line has free memory, load and cores", () => {
    const mac = telemetry({ memory_pressure: { level: "warn", free_percent: 41.6 }, cpu: { load1: 2.14, per_core_percent: [1, 2, 3, 4] } });
    expect(usesMemoryPressure(mac)).toBe(true);
    expect(memoryPressureMeterLabel("warn")).toBe("Memory pressure Warning");
    expect(memoryPressureMicro(mac)).toBe("42% free · Load 2.1 · 4 cores");
    expect(memoryPressureMicro(telemetry({ memory_pressure: { level: "normal" }, cpu: { load1: 1 } }))).toBe("—");
    const linux = telemetry({ memory_pressure: { level: "warn" }, psi: { cpu: { some: { avg10: 1, avg60: 0, avg300: 0 } } } });
    expect(usesMemoryPressure(linux)).toBe(false);
  });
});

describe("consumer rows", () => {
  test("a Worker shows its status; an orphan is residue; an unknown status says nothing", () => {
    expect(consumerStatus(consumer({ status: "running" }))).toBe("running");
    expect(consumerStatus(consumer({ status: "unknown" }))).toBeNull();
    expect(consumerStatus(consumer({ owner: "orphaned_worker", status: "exited" }))).toBe("Residual · exited");
    expect(consumerStatus(consumer({ owner: "orphaned_worker" }))).toBe("Residual · unknown");
    expect(consumerRowMeta(consumer({ status: "running", executor: "codex", cwd: "/repo", ageSeconds: 30 }))).toBe("running · codex · /repo · just now");
  });

  test("the row reads aloud whether it expands or collapses, its owner, status, CPU, memory and processes", () => {
    expect(consumerAccessibilityLabel(consumer({ status: "running" }), "Build", false)).toBe("Expand Build, Worker, running, CPU 12%, RSS 1.5 GB, 3 processes");
    expect(consumerAccessibilityLabel(consumer({}), "Build", true)).toBe("Collapse Build, Worker, unknown, CPU 12%, RSS 1.5 GB, 3 processes");
  });

  test("rows with the same identity keep keys of their own", () => {
    const twin = consumer({ id: "worker:a" });
    expect(consumerRowKeys([twin, twin, consumer({ id: "worker:b" })])).toEqual(["worker:worker:a:0", "worker:worker:a:1", "worker:worker:b:0"]);
  });

  test("commands merge the processes' commands once; processes go largest first, unknown memory last", () => {
    const c = consumer({ commands: ["java"], processes: [{ pid: 1, command: "java" }, { pid: 2, command: "node", rssBytes: 10 }, { pid: 3, rssBytes: 20 }] });
    expect(consumerCommands(c)).toEqual(["java", "node"]);
    expect(processesByMemory(c).map((p) => p.pid)).toEqual([3, 2, 1]);
  });

  test("a consumer's title, IDs and commands can be copied, and a process's PID, command and start token", () => {
    const c = consumer({ id: "worker:a", workerId: "w1", workId: "k1", commands: ["java"] });
    expect(consumerCopyChoices(c, "Build")).toEqual([
      { label: "Copy title", value: "Build" },
      { label: "Copy ID", value: "worker:a" },
      { label: "Copy Worker ID", value: "w1" },
      { label: "Copy Work ID", value: "k1" },
      { label: "Copy commands", value: "java" },
    ]);
    expect(processCopyChoices({ pid: 18420, command: "java", start: "918204" })).toEqual([
      { label: "Copy PID", value: "18420" },
      { label: "Copy command", value: "java" },
      { label: "Copy start token", value: "918204" },
    ]);
    expect(processCopyChoices({})).toEqual([]);
  });
});
