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
