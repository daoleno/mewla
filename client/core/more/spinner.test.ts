import { describe, expect, test } from "bun:test";
import { spinnerRadius } from "./spinner";

describe("the spinner's arc", () => {
  test("fits inside its canvas", () => {
    expect(spinnerRadius(20, 20)).toBe(8.5);
    expect(spinnerRadius(16, 24)).toBe(6.5);
  });

  test("is not drawn in a canvas with no room (0×0 before layout or while hidden)", () => {
    expect(spinnerRadius(0, 0)).toBeNull();
    expect(spinnerRadius(3, 3)).toBeNull();
  });
});
