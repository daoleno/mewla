import { describe, expect, test } from "bun:test";
import type { PhoneLinkOptions } from "./phoneLink";
import { PhoneLinkHost, untilLinkSettles } from "./phoneLinkHost";

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

describe("untilLinkSettles", () => {
  function timers() {
    const pending = new Map<number, () => void>();
    let next = 0;
    return {
      set: (fn: () => void) => {
        pending.set(++next, fn);
        return next;
      },
      clear: (handle: unknown) => void pending.delete(handle as number),
      fire: () => [...pending.values()].forEach((fn) => fn()),
      count: () => pending.size,
    };
  }

  test("a headless task ends when the link comes up, and lets go of its timer and watch", async () => {
    const { host, made, options } = setup();
    const t = timers();
    host.hold(options("a"));
    const settled = untilLinkSettles(host, 40_000, t);
    expect(t.count()).toBe(1);
    made[0].report("connecting");
    made[0].report("online");
    expect(await settled).toBe("online");
    expect(t.count()).toBe(0);
  });

  test("it also ends for a link that can never come up by itself, and at the cap while the computer is away", async () => {
    const { host, made, options } = setup();
    host.hold(options("a"));
    const replaced = untilLinkSettles(host, 40_000, timers());
    made[0].report("replaced");
    expect(await replaced).toBe("replaced");

    const away = setup();
    away.host.hold(away.options("b"));
    const t = timers();
    const waiting = untilLinkSettles(away.host, 40_000, t);
    away.made[0].report("offline");
    t.fire();
    expect(await waiting).toBe("offline");
  });

  test("a link that is already up ends it at once", async () => {
    const { host, made, options } = setup();
    host.hold(options("a"));
    made[0].report("online");
    const t = timers();
    expect(await untilLinkSettles(host, 40_000, t)).toBe("online");
    expect(t.count()).toBe(0);
  });
});
