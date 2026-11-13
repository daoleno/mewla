import { describe, expect, test } from "bun:test";
import { motion, bezier } from "./tokens";
import { loops, play, settle } from "./transitions";
import { ActionWait, FORCED, LinkWatch, optimistic, Wait, type Clock, type WaitState } from "./wait";

/** A manual clock: time moves only by `run(ms)`, timers fire in order. */
function manualClock() {
  let now = 0;
  let seq = 0;
  const timers = new Map<number, { at: number; run: () => void; seq: number }>();
  const clock: Clock = {
    now: () => now,
    later: (ms, run) => {
      const id = ++seq;
      timers.set(id, { at: now + Math.max(0, ms), run, seq: id });
      return id;
    },
    cancel: (handle) => void timers.delete(handle as number),
  };
  const run = (ms: number) => {
    const end = now + ms;
    for (;;) {
      const due = [...timers.entries()].filter(([, t]) => t.at <= end).sort((a, b) => a[1].at - b[1].at || a[1].seq - b[1].seq)[0];
      if (!due) break;
      timers.delete(due[0]);
      now = due[1].at;
      due[1].run();
    }
    now = end;
  };
  return { clock, run, get now() { return now; } };
}

function place(options: { failMs?: number; link?: "up" | "connecting" | "away" } = {}) {
  const c = manualClock();
  const seen: Array<WaitState & { at: number }> = [];
  let reads = 0;
  const w = new Wait({ ...options, clock: c.clock, read: () => void reads++, onChange: (s) => seen.push({ ...s, at: c.now }) });
  return { w, c, seen, reads: () => reads, view: () => w.current.view, slow: () => w.current.slow };
}

describe("tokens: SPEC 16.1's values under its names", () => {
  test("waits, motion and the spring", () => {
    expect(motion.wait).toEqual({ quiet: 150, hold: 300, slow: 3000, fail: 15000, person: 600000 });
    expect(motion.link.away).toBe(10000);
    expect(motion.dur).toEqual({ fast: 120, base: 200, move: 300 });
    expect(motion.ease.enter).toEqual([0.2, 0.8, 0.2, 1]);
    expect(motion.spring).toEqual({ stiffness: 380, damping: 39, mass: 1 });
    expect(motion.loop).toEqual({ spin: 900, slide: 1400 });
    expect(motion.send.giveup).toBe(120000);
    expect(bezier("move")).toBe("cubic-bezier(0.4, 0, 0.2, 1)");
  });
});

describe("a place reading its data (the reference page's demo A)", () => {
  test("an answer inside wait.quiet draws nothing but the content", () => {
    const p = place();
    p.w.start();
    p.c.run(100);
    p.w.resolve();
    expect(p.seen.map((s) => s.view)).not.toContain("placeholder");
    expect(p.view()).toBe("content");
  });

  test("nothing is drawn before 150 ms; then the placeholder, held 300 ms once drawn", () => {
    const p = place();
    p.w.start();
    p.c.run(149);
    expect(p.view()).toBe("none");
    p.c.run(1);
    expect(p.view()).toBe("placeholder");
    p.c.run(50); // 200 ms: the answer comes 50 ms after the placeholder was drawn
    p.w.resolve();
    expect(p.view()).toBe("placeholder");
    p.c.run(249);
    expect(p.view()).toBe("placeholder");
    p.c.run(1);
    expect(p.view()).toBe("content");
  });

  test("a normal 0.7 s read: placeholder, then content, no slow line", () => {
    const p = place();
    p.w.start();
    p.c.run(700);
    p.w.resolve();
    expect(p.view()).toBe("content");
    expect(p.seen.some((s) => s.slow)).toBe(false);
  });

  test("the slow line at 3 s, gone with the answer", () => {
    const p = place();
    p.w.start();
    p.c.run(2999);
    expect(p.slow()).toBe(false);
    p.c.run(1);
    expect(p.slow()).toBe(true);
    p.c.run(2000);
    p.w.resolve();
    expect(p.slow()).toBe(false);
    expect(p.view()).toBe("content");
  });

  test("no answer on a live link: Try again's failed state at 15 s, not before", () => {
    const p = place();
    p.w.start();
    p.c.run(14999);
    expect(p.view()).toBe("placeholder");
    p.c.run(1);
    expect(p.view()).toBe("failed");
    expect(p.slow()).toBe(false);
  });

  test("a place's own bound (approve 8 s, git 90 s) replaces wait.fail", () => {
    const p = place({ failMs: 90_000 });
    p.w.start();
    p.c.run(15_000);
    expect(p.view()).toBe("placeholder");
    p.c.run(75_000);
    expect(p.view()).toBe("failed");
  });

  test("an error answer fails at once (after the placeholder's hold)", () => {
    const p = place();
    p.w.start();
    p.c.run(900);
    p.w.fail();
    expect(p.view()).toBe("failed");
  });

  test("empty only after a read that succeeded", () => {
    const p = place();
    p.w.start();
    p.c.run(20_000);
    expect(p.seen.map((s) => s.view)).not.toContain("empty");
    p.w.retry();
    expect(p.reads()).toBe(1);
    p.c.run(700);
    p.w.resolve("empty");
    expect(p.view()).toBe("empty");
  });

  test("Try again reads again: the placeholder, not the failure, while it waits", () => {
    const p = place();
    p.w.start();
    p.c.run(15_000);
    expect(p.view()).toBe("failed");
    p.w.retry();
    p.c.run(150);
    expect(p.view()).toBe("placeholder");
  });

  test("a stale read's answer is ignored", () => {
    const p = place();
    const first = p.w.start();
    p.c.run(200);
    p.w.start();
    p.w.resolve("content", first);
    expect(p.view()).not.toBe("content");
  });
});

describe("last known first", () => {
  test("a place showing its last known content refreshes underneath: no placeholder, no flash", () => {
    const p = place();
    p.w.showKnown();
    p.w.start();
    p.c.run(1000);
    p.w.resolve();
    expect(p.seen.every((s) => s.view === "content")).toBe(true);
  });

  test("its refresh failing keeps the content", () => {
    const p = place();
    p.w.showKnown();
    p.w.start();
    p.c.run(15_000);
    expect(p.view()).toBe("content");
    expect(p.w.current.reading).toBe(false);
  });

  test("its slow refresh still shows the slow line", () => {
    const p = place();
    p.w.showKnown();
    p.w.start();
    p.c.run(3000);
    expect(p.slow()).toBe(true);
    expect(p.view()).toBe("content");
  });
});

describe("the link: offline is not a failure", () => {
  test("a read while connecting waits (placeholder, slow line) and never fails", () => {
    const p = place({ link: "connecting" });
    p.w.start();
    p.c.run(150);
    expect(p.view()).toBe("placeholder");
    p.c.run(60_000);
    expect(p.view()).toBe("placeholder");
    expect(p.slow()).toBe(true);
  });

  test("a read the link interrupts waits for it, then shows away when the link is away", () => {
    const p = place();
    p.w.start();
    p.c.run(1000);
    p.w.setLink("connecting");
    p.c.run(20_000);
    expect(p.view()).toBe("placeholder");
    p.w.setLink("away");
    expect(p.view()).toBe("away");
  });

  test("the link back: the waiting or away place reads again by itself", () => {
    const p = place();
    p.w.start();
    p.w.setLink("connecting");
    p.w.setLink("away");
    expect(p.reads()).toBe(0);
    p.w.setLink("up");
    expect(p.reads()).toBe(1);
    p.c.run(700);
    p.w.resolve();
    expect(p.view()).toBe("content");
  });

  test("last known content while away is dimmed, kept, and refreshed when back", () => {
    const p = place();
    p.w.showKnown();
    p.w.setLink("connecting");
    expect(p.w.current.dim).toBe(false);
    p.w.setLink("away");
    expect(p.view()).toBe("content");
    expect(p.w.current.dim).toBe(true);
    p.w.setLink("up");
    expect(p.w.current.dim).toBe(false);
    expect(p.reads()).toBe(1);
  });

  test("an error answer while the link is down waits for the link", () => {
    const p = place();
    p.w.start();
    p.w.setLink("connecting");
    p.w.fail();
    expect(p.view()).not.toBe("failed");
  });

  test("Try again while away asks the link, not the read", () => {
    const p = place({ link: "away" });
    p.w.start();
    let resumed = 0;
    p.w.retry(() => resumed++);
    expect(resumed).toBe(1);
    expect(p.reads()).toBe(0);
  });
});

describe("the link as the person is told it", () => {
  test("Offline at 10 s of connecting, kept between retries, up only with a socket", () => {
    const c = manualClock();
    const seen: string[] = [];
    const l = new LinkWatch((s) => seen.push(s), c.clock);
    l.update("connecting");
    c.run(9999);
    expect(l.current).toBe("connecting");
    c.run(1);
    expect(l.current).toBe("away");
    l.update("waiting");
    l.update("connecting");
    l.update("waiting");
    expect(l.current).toBe("away");
    l.update("connected");
    expect(l.current).toBe("up");
    expect(seen).toEqual(["away", "up"]);
  });

  test("a short drop never says Offline; a cold start is connecting first", () => {
    const c = manualClock();
    const l = new LinkWatch(undefined, c.clock);
    l.update("idle");
    expect(l.current).toBe("connecting");
    l.update("connected");
    l.update("waiting");
    c.run(3000);
    l.update("connected");
    expect(l.current).toBe("up");
    c.run(20_000);
    expect(l.current).toBe("up");
  });
});

describe("a button that waits (not optimistic)", () => {
  test("the press alone under 150 ms, the spinner after, failed at the action's bound", () => {
    const c = manualClock();
    const a = new ActionWait({ failMs: 8000, clock: c.clock });
    a.press();
    expect(a.phase).toBe("pressed");
    expect(a.busy).toBe(true);
    c.run(149);
    expect(a.phase).toBe("pressed");
    c.run(1);
    expect(a.phase).toBe("spinning");
    c.run(7850);
    expect(a.phase).toBe("failed");
    expect(a.busy).toBe(false);
  });

  test("an answer inside 150 ms never shows the spinner", () => {
    const c = manualClock();
    const phases: string[] = [];
    const a = new ActionWait({ clock: c.clock, onChange: (p) => phases.push(p) });
    a.press();
    c.run(80);
    a.done();
    c.run(1000);
    expect(phases).toEqual(["pressed", "idle"]);
  });
});

describe("optimistic, with its way back", () => {
  test("shown at once, kept when the computer agrees", async () => {
    const c = manualClock();
    let value = false;
    const held = await optimistic({ prev: false, next: true, apply: (v) => (value = v), commit: async () => {}, clock: c.clock });
    expect(held).toBe(true);
    expect(value).toBe(true);
  });

  test("rolled back with a word when it refuses", async () => {
    const c = manualClock();
    let value = false;
    let told = 0;
    const held = await optimistic({ prev: false, next: true, apply: (v) => (value = v), commit: () => Promise.reject(new Error("no")), onFailed: () => told++, clock: c.clock });
    expect(held).toBe(false);
    expect(value).toBe(false);
    expect(told).toBe(1);
  });

  test("rolled back at wait.fail without an answer", async () => {
    const c = manualClock();
    let value = "Old";
    const pending = optimistic({ prev: "Old", next: "New", apply: (v) => (value = v), commit: () => new Promise(() => {}), clock: c.clock });
    expect(value).toBe("New");
    c.run(motion.wait.fail);
    expect(await pending).toBe(false);
    expect(value).toBe("Old");
  });
});

describe("dev builds force a state", () => {
  test("slow: the place reads cold, placeholder and slow line, content at 5 s", () => {
    const p = place();
    p.w.showKnown();
    p.w.setForce("slow");
    p.c.run(150);
    expect(p.view()).toBe("placeholder");
    p.c.run(200);
    p.w.resolve();
    p.c.run(2650);
    expect(p.slow()).toBe(true);
    p.c.run(FORCED.slowMs - 3000);
    expect(p.view()).toBe("content");
  });

  test("no answer: failed at 15 s; error: failed at once", () => {
    const p = place();
    p.w.setForce("none");
    p.w.resolve();
    p.c.run(15_000);
    expect(p.view()).toBe("failed");
    const q = place();
    q.w.setForce("error");
    q.c.run(FORCED.errorMs + motion.wait.hold);
    expect(q.view()).toBe("failed");
  });

  test("offline: connecting, away at 10 s; cleared, it reads again", () => {
    const p = place();
    p.w.start();
    p.w.resolve();
    p.w.setForce("offline");
    p.c.run(9999);
    expect(p.w.current.dim).toBe(false);
    p.c.run(1);
    expect(p.w.current.dim).toBe(true);
    const before = p.reads();
    p.w.setForce(null);
    expect(p.reads()).toBe(before + 1);
    expect(p.w.current.dim).toBe(false);
  });
});

describe("reduced motion: fades or cuts", () => {
  test("every move becomes a fast fade without travel or size", () => {
    expect(play("arrive", false).rise).toBe(8);
    expect(play("arrive", true)).toEqual({ duration: 120, easing: "linear", rise: 0, scale: 1, size: false });
    expect(play("open", true).size).toBe(false);
    expect(play("jumpIn", true).scale).toBe(1);
  });

  test("the slow line's pass stops; the spinner turns; the spring is a cut", () => {
    expect(loops("slide", true)).toBe(false);
    expect(loops("spin", true)).toBe(true);
    expect(settle(400, true)).toBeNull();
    expect(settle(400, false)).toEqual({ stiffness: 380, damping: 39, mass: 1, velocity: 400 });
  });
});

describe("no endless wait for an action", () => {
  test("bounded rejects with NoAnswer at wait.fail; an answer before keeps its value", async () => {
    const { bounded, NoAnswer } = await import("./wait");
    const c = manualClock();
    const never = bounded(new Promise(() => {}), undefined, c.clock);
    c.run(motion.wait.fail);
    await expect(never).rejects.toBeInstanceOf(NoAnswer);
    expect(await bounded(Promise.resolve(7), undefined, c.clock)).toBe(7);
  });
});
