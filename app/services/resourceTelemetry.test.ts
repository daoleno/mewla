import { expect, test } from "bun:test";
import {
  normalizeResourceTelemetry, cpuHistorySeries, memoryHistorySeries,
  diskThroughputSeries, formatCores, formatPercent, groupConsumers,
} from "./resourceTelemetry";

test("v2 snake-case metrics retain measured zeros, ordered history and per-core consumer CPU", () => {
  const sample = normalizeResourceTelemetry({
    version: 2, sampled_at: "2026-10-01T06:00:10Z", state: "elevated",
    cpu: { utilization_percent: 25, per_core_percent: [0, 50] },
    memory: { total_bytes: 1000, available_bytes: 400, swap_used_bytes: 0 },
    history: [
      { sampled_at: "2026-10-01T06:00:10Z", cpu_percent: 25, memory_used_bytes: 600, disk_read_bytes_per_second: 0 },
      { sampled_at: "2026-10-01T06:00:05Z", cpu_percent: 0, memory_used_bytes: 500 },
    ],
    consumers: [{ owner: "worker", cpu_percent: 250, rss_bytes: 100 }],
  })!;
  expect(cpuHistorySeries(sample).map((point) => point.value)).toEqual([0, 0.25]);
  expect(memoryHistorySeries(sample).map((point) => point.value)).toEqual([0.5, 0.6]);
  expect(diskThroughputSeries(sample).read.map((point) => point.value)).toEqual([0]);
  expect(diskThroughputSeries(sample).write).toEqual([]);
  expect(formatCores(sample.consumers[0].cpuPercent)).toBe("2.5 cores");
});

test("unavailable platform and first-interval measurements never become measured zero", () => {
  const sample = normalizeResourceTelemetry({
    version: 2, sampled_at: "2026-10-01T06:00:00Z", unavailable: ["cpu", "memory"],
    consumers: [{ owner: "user", rss_bytes: 100 }],
  })!;
  expect(formatPercent(sample.cpu.utilizationPercent)).toBe("—");
  expect(sample.memory.totalBytes).toBeUndefined();
  expect(sample.psi).toEqual({ cpu: undefined, memory: undefined, io: undefined });
  expect(cpuHistorySeries(sample)).toEqual([]);
  expect(formatCores(sample.consumers[0].cpuPercent)).toBe("—");
  expect(groupConsumers(sample.consumers)[0].cpuPercent).toBeUndefined();
  expect(formatCores(0)).toBe("idle");
  expect(normalizeResourceTelemetry({ sampled_at: "invalid" })).toBeNull();
});

// Synthetic contract fixture. No device identity, commands with arguments, or daemon state.
import dashboard from "../__fixtures__/telemetry/dashboard-v2.json";
import { consumerKey, consumerTitle, sortConsumers } from "./resourceTelemetry";
const consumer = (raw: unknown) => normalizeResourceTelemetry({ sampled_at: dashboard.sampled_at, consumers: [raw] })!.consumers[0];

test("v2 preserves exact identity, executable labels and opaque process-generation tokens", () => {
  const snapshot = normalizeResourceTelemetry(dashboard)!;
  const build = snapshot.consumers.find((c) => c.owner === "worker")!;
  expect(build.id).toBe("worker:android-build");
  expect(build.commands).toEqual(["java", "node"]);
  expect(build.processes[0]).toEqual({ pid: 18420, start: "918204", command: "java", rssBytes: 5 * 1024 ** 3 });
  expect(build.processCount).toBe(18);
  expect(build.status).toBe("running");
  expect(build.workId).toBe("work-release");
  expect(consumerTitle(build)).toBe("Android release build");
  expect(consumerTitle(snapshot.consumers.find((c) => c.owner === "docker")!)).toBe("8f4e6b921d7a · postgres");
  expect(snapshot.consumers.find((c) => c.owner === "orphaned_worker")?.status).toBe("done");
});

test("name priority covers title, work/worker titles, commands, process, Docker id, executor and PID", () => {
  const examples: [unknown, string][] = [
    [{ title: "Human title", commands: ["node"] }, "Human title"],
    [{ work_title: "Fix API", executor: "codex", owner: "worker" }, "Fix API"],
    [{ worker_title: "Review", owner: "worker" }, "Review"],
    [{ commands: ["", " node ", "java"] }, "node"],
    [{ processes: [{ command: "postgres", pid: 8 }] }, "postgres"],
    [{ owner: "docker", id: "docker:abc123" }, "abc123"],
    [{ owner: "docker", commands: ["redis-server"] }, "redis-server"],
    [{ owner: "worker", work_id: "work-a" }, "Work work-a"],
    [{ owner: "worker", worker_id: "worker-a" }, "Worker worker-a"],
    [{ owner: "worker", executor: "codex" }, "Codex · Worker"],
    [{ owner: "brain", executor: "codex" }, "Codex · Brain"],
    [{ owner: "orphaned_worker", executor: "claude" }, "Claude · Orphaned Worker"],
    [{ processes: [{ pid: 42 }] }, "Unknown process · PID 42"],
    [{ owner: "docker" }, "Unknown process"],
    [{}, "Unknown process"],
  ];
  for (const [raw, expected] of examples) expect(consumerTitle(consumer(raw))).toBe(expected);
});

test("malformed fields and old telemetry remain safe without inventing process details", () => {
  const old = consumer({ owner: "worker", title: "Legacy task", rss_bytes: 40, kinds: ["node"] });
  expect(old.commands).toEqual([]);
  expect(old.processes).toEqual([]);
  expect(old.processCount).toBe(0);
  const malformed = consumer({ id: {}, title: [], commands: [4, null, "node", "node"],
    processes: [null, 1, { pid: -2, start: 123, command: {}, rss_bytes: NaN }, { pid: 2.5 }, { pid: 42, rss_bytes: 0 }] });
  expect(malformed.commands).toEqual(["node"]);
  expect(malformed.processes[0]).toEqual({ pid: undefined, start: undefined, command: undefined, rssBytes: undefined });
  expect(malformed.processes[1].pid).toBeUndefined();
  expect(malformed.processes[2].rssBytes).toBe(0);
  expect(malformed.processCount).toBe(3);
  expect(consumerKey(old)).toBe(consumerKey({ ...old, rssBytes: 999, cpuPercent: 100 }));
  expect(normalizeResourceTelemetry({ sampled_at: dashboard.sampled_at })?.version).toBe(1);
});

test("resource sorting is global, keeps missing CPU last, and preserves all owner groups", () => {
  const snapshot = normalizeResourceTelemetry(dashboard)!;
  const original = [...snapshot.consumers];
  const cpu = sortConsumers(snapshot.consumers, "cpu");
  expect(cpu[0].title).toBe("Android release build");
  expect(cpu[1].owner).toBe("user");
  expect(cpu.at(-1)?.cpuPercent).toBeUndefined();
  expect(sortConsumers(snapshot.consumers, "rss")[0].rssBytes).toBe(7 * 1024 ** 3);
  expect(snapshot.consumers).toEqual(original);
  expect(groupConsumers(snapshot.consumers).map((g) => g.title)).toEqual(["Workers", "Orphaned Workers", "Brain", "Docker", "User processes"]);
});

import { latestDiskThroughput, formatPressurePercent } from "./resourceTelemetry";
test("disk current values never borrow an old interval; history keeps current-only and write-only samples", () => {
  const base = { sampled_at: "2026-10-01T06:00:10Z", history: [{ sampled_at: "2026-10-01T06:00:05Z", disk_read_bytes_per_second: 99 }] };
  expect(latestDiskThroughput(normalizeResourceTelemetry(base)!)).toEqual({ read: undefined, write: undefined });
  const sample = normalizeResourceTelemetry({ ...base, disks: [{ mount: "/", write_bytes_per_second: 2 ** 22 }] })!;
  expect(latestDiskThroughput(sample)).toEqual({ read: undefined, write: 2 ** 22 });
  expect(diskThroughputSeries(sample).write).toEqual([{ at: sample.sampledAt, value: 1 }]);
  expect(formatPressurePercent(0.15)).toBe("0.15%");
  expect(formatPressurePercent(undefined)).toBe("—");
});
