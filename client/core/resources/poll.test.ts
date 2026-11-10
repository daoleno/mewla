import { describe, expect, test } from "bun:test";
import type { Daemon } from "../more/daemon";
import { RESOURCE_POLL_MS, ResourcePoller, type Timers } from "./poll";

function clock() {
  const queue: Array<{ at: number; run: () => void; id: number }> = [];
  let now = 0;
  let next = 1;
  const timers: Timers = {
    setTimeout: (run, ms) => {
      const id = next++;
      queue.push({ at: now + ms, run, id });
      return id;
    },
    clearTimeout: (id) => {
      const index = queue.findIndex((entry) => entry.id === id);
      if (index >= 0) queue.splice(index, 1);
    },
  };
  return {
    timers,
    pending: () => queue.length,
    advance(ms: number) {
      now += ms;
      for (const entry of queue.filter((e) => e.at <= now)) {
        queue.splice(queue.indexOf(entry), 1);
        entry.run();
      }
    },
  };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

function daemon(answer: (n: number, signal?: AbortSignal) => Promise<unknown>) {
  let n = 0;
  const d: Daemon & { asked: number } = {
    asked: 0,
    server: { id: "s1", name: "Desk", url: "ws://x/ws", daemonId: "", daemonPublicKey: "" },
    state: "connected",
    on: () => () => {},
    onState: () => () => {},
    send: () => {},
    request: (async (_type: string, _body: unknown, options: { signal?: AbortSignal }) => {
      d.asked++;
      return answer(n++, options.signal);
    }) as Daemon["request"],
  };
  return d;
}

const sample = (sampledAt: string) => ({ version: 2, sampled_at: sampledAt, state: "normal", cpu: {}, memory: { total_bytes: 8, available_bytes: 4 } });

describe("reading resources", () => {
  test("reads at once, then five seconds after each answer, one at a time", async () => {
    const c = clock();
    const d = daemon(async () => sample("2026-11-11T00:00:00Z"));
    const seen: boolean[] = [];
    const poller = new ResourcePoller(d, (s) => seen.push(s.reading), c.timers);
    poller.start();
    poller.refresh();
    await flush();
    expect(d.asked).toBe(1);
    expect(poller.current.telemetry?.memory.totalBytes).toBe(8);
    c.advance(RESOURCE_POLL_MS - 1);
    expect(d.asked).toBe(1);
    c.advance(1);
    await flush();
    expect(d.asked).toBe(2);
    poller.stop();
    expect(c.pending()).toBe(0);
  });

  test("stopping cancels the read in flight, and its late answer changes nothing", async () => {
    const c = clock();
    let release: (value: unknown) => void = () => {};
    const d = daemon((_n, signal) => new Promise((resolve, reject) => {
      release = resolve;
      signal?.addEventListener("abort", () => reject(new Error("get_resource_telemetry cancelled")));
    }));
    const poller = new ResourcePoller(d, () => {}, c.timers);
    poller.start();
    poller.stop();
    release(sample("2026-11-11T00:00:00Z"));
    await flush();
    expect(poller.current.telemetry).toBeNull();
    expect(poller.current.reading).toBe(false);
    expect(c.pending()).toBe(0);
  });

  test("a failure keeps the last reading and says why; the next answer clears it", async () => {
    const c = clock();
    const d = daemon(async (n) => {
      if (n === 1) throw new Error("get_resource_telemetry timed out");
      return sample(n === 0 ? "2026-11-11T00:00:00Z" : "2026-11-11T00:00:10Z");
    });
    const poller = new ResourcePoller(d, () => {}, c.timers);
    poller.start();
    await flush();
    c.advance(RESOURCE_POLL_MS);
    await flush();
    expect(poller.current.error).toBe("Resource telemetry timed out.");
    expect(poller.current.telemetry).not.toBeNull();
    c.advance(RESOURCE_POLL_MS);
    await flush();
    expect(poller.current.error).toBe("");
  });

  test("an answer older than the reading held is ignored; an answer that is no reading fails", async () => {
    const c = clock();
    const d = daemon(async (n) => (n === 0 ? sample("2026-11-11T00:00:10Z") : n === 1 ? sample("2026-11-11T00:00:00Z") : { nonsense: true }));
    const poller = new ResourcePoller(d, () => {}, c.timers);
    poller.start();
    await flush();
    const first = poller.current.telemetry?.sampledAt;
    c.advance(RESOURCE_POLL_MS);
    await flush();
    expect(poller.current.telemetry?.sampledAt).toBe(first!);
    c.advance(RESOURCE_POLL_MS);
    await flush();
    expect(poller.current.error).toBe("Invalid resource telemetry.");
  });
});
