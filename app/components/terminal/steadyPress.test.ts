import { expect, test } from "bun:test";
import { createSteadyPress } from "./steadyPress";

function setup(slop?: number) {
  const pressed: { pageX: number; pageY: number; locationX: number; locationY: number }[] = [];
  const active: boolean[] = [];
  const press = createSteadyPress({
    onPress: (touch) => pressed.push(touch),
    onActiveChange: (value) => active.push(value),
    slop,
  });
  const at = (pageX: number, pageY: number, locationX = 5, locationY = 6) => ({
    nativeEvent: { pageX, pageY, locationX, locationY },
  });
  return { press, pressed, active, at };
}

test("a press commits on release with where it landed", () => {
  const { press, pressed, active, at } = setup();
  press.grant(at(100, 200, 18, 16));
  press.release();
  expect(pressed).toEqual([{ pageX: 100, pageY: 200, locationX: 18, locationY: 16 }]);
  expect(active).toEqual([true, false]);
});

test("finger jitter inside the slop still commits, however many moves", () => {
  const { press, pressed, at } = setup();
  press.grant(at(100, 200));
  for (const [dx, dy] of [[1, 1], [3, -2], [-4, 5], [9, 9], [0, 0]]) press.move(at(100 + dx, 200 + dy));
  expect(press.canTerminate()).toBe(false);
  press.release();
  expect(pressed).toHaveLength(1);
});

test("a drag beyond the slop is not a press and hands the touch over", () => {
  const { press, pressed, active, at } = setup();
  press.grant(at(100, 200));
  press.move(at(100, 211));
  expect(press.canTerminate()).toBe(true);
  expect(active).toEqual([true, false]);
  press.release();
  expect(pressed).toEqual([]);
  // Coming back does not revive it.
  press.grant(at(100, 200));
  press.move(at(100, 230));
  press.move(at(100, 200));
  press.release();
  expect(pressed).toEqual([]);
});

test("a terminated touch commits nothing, and a stray release is ignored", () => {
  const { press, pressed, at } = setup();
  press.grant(at(10, 10));
  press.terminate();
  press.release();
  press.release();
  expect(pressed).toEqual([]);
});

test("each touch starts clean", () => {
  const { press, pressed, at } = setup();
  press.grant(at(10, 10));
  press.move(at(10, 80));
  press.release();
  press.grant(at(10, 10));
  press.release();
  expect(pressed).toHaveLength(1);
});
