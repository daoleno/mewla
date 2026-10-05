import { expect, test } from "bun:test";
import { loadedImageRatio } from "./zenImageRatio";

test("reads the native source size", () => {
  expect(loadedImageRatio({ source: { width: 400, height: 200 } })).toBe(2);
});

test("reads the web image element when there is no source", () => {
  expect(loadedImageRatio({ target: { naturalWidth: 412, naturalHeight: 915 } })).toBeCloseTo(412 / 915);
});

test("falls back to square for unknown or empty sizes", () => {
  expect(loadedImageRatio(undefined)).toBe(1);
  expect(loadedImageRatio({})).toBe(1);
  expect(loadedImageRatio({ source: { width: 0, height: 10 } })).toBe(1);
});
