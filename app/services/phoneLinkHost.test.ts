import { describe, expect, test } from "bun:test";
import type { PhoneLinkOptions } from "./phoneLink";
import { PhoneLinkHost } from "./phoneLinkHost";

function serverNamed(id: string) {
  return { id } as unknown as PhoneLinkOptions["server"];
}

function setup() {
  const made: { id: string; started: number; stopped: number; report(state: Parameters<PhoneLinkOptions["onState"]>[0]): void }[] = [];
  const host = new PhoneLinkHost((options) => {
    const entry = { id: options.server.id, started: 0, stopped: 0, report: options.onState };
    made.push(entry);
    return { start: () => void (entry.started += 1), stop: () => void (entry.stopped += 1) };
  });
  const options = (id: string) => ({ server: serverNamed(id), native: {} as PhoneLinkOptions["native"], appVersion: "1" });
  return { host, made, options };
}

describe("phone link host", () => {
  test("one link per computer: holding the same server again keeps it, so a screen and a headless start never trade closes", () => {
    const { host, made, options } = setup();
    host.hold(options("a"));
    host.hold(options("a"));
    expect(made).toHaveLength(1);
    expect(made[0].started).toBe(1);
    expect(host.serverId).toBe("a");
  });

  test("a server switch closes the old link before the new one opens", () => {
    const { host, made, options } = setup();
    host.hold(options("a"));
    host.hold(options("b"));
    expect(made.map((link) => [link.id, link.started, link.stopped])).toEqual([["a", 1, 1], ["b", 1, 0]]);
  });

  test("watchers hear the state, and a released link says nothing more", () => {
    const { host, made, options } = setup();
    const seen: string[] = [];
    const stop = host.watch((state) => seen.push(state));
    host.hold(options("a"));
    made[0].report("online");
    host.release();
    made[0].report("offline");
    stop();
    expect(seen).toEqual(["off", "online", "off"]);
    expect(host.current).toBe("off");
    expect(host.serverId).toBeNull();
  });
});
