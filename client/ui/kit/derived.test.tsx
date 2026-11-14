import { computed, effect, signal } from "nsh";
import { describe, expect, test } from "nsh/test";
import { derived } from "./derived";

// Each change in a call of its own, as a socket's message or a timer is: NSH's 2 s budget is per call into JS.
// It resolves first: a call NSH cuts off ends there, catch blocks and all.
const later = (fn: () => void) => new Promise<void>((resolve) => setTimeout(() => {
  resolve();
  try {
    fn();
  } catch {
    // The run that failed: what counts is the next one.
  }
}, 5));
const settle = () => new Promise((resolve) => setTimeout(resolve, 20));
const spin = (ms: number) => {
  const end = Date.now() + ms;
  while (Date.now() < end);
};

async function threeChanges(source: { set(value: number): void }) {
  await later(() => source.set(1));
  await settle();
  await later(() => source.set(2));
  await settle();
  await later(() => source.set(3));
  await settle();
}

describe("a value made from signals, after one run that failed", () => {
  test("NSH's computed: what reads it never hears of a change again (why derived exists; if this fails, NSH fixed it)", async () => {
    const source = signal(0);
    const made = computed(() => {
      if (source() === 1) throw new Error("failed once");
      return source();
    });
    const seen: number[] = [];
    effect(() => {
      seen.push(made());
    });
    await settle();
    await threeChanges(source);
    expect(seen).toEqual([0]);
  });

  test("derived: an error keeps the last value, and the next change makes it again", async () => {
    const source = signal(0);
    const made = derived(() => {
      if (source() === 1) throw new Error("failed once");
      return source() * 10;
    });
    const seen: number[] = [];
    effect(() => {
      seen.push(made());
    });
    await settle();
    await threeChanges(source);
    expect(seen).toEqual([0, 20, 30]);
  });

  test("derived: a run NSH cuts off after 2 s keeps the last value, and the next change makes it again", async () => {
    const source = signal(0);
    const made = derived(() => {
      if (source() === 1) spin(2600);
      return source() * 10;
    });
    const seen: number[] = [];
    effect(() => {
      seen.push(made());
    });
    await settle();
    await later(() => source.set(1));
    await new Promise((resolve) => setTimeout(resolve, 3000));
    await later(() => source.set(2));
    await settle();
    expect(seen).toEqual([0, 20]);
  }, 20_000);

  test("derived: a value equal by `equals` is not news", async () => {
    const source = signal(1);
    const made = derived(() => ({ even: source() % 2 === 0 }), { equals: (a, b) => a.even === b.even });
    const seen: boolean[] = [];
    effect(() => {
      seen.push(made().even);
    });
    await settle();
    await threeChanges(source);
    expect(seen).toEqual([false, true, false]);
  });
});
