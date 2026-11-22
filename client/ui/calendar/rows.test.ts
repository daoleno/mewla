import { describe, expect, test } from "nsh/test";
import { timeColumnWidth } from "./rows";

// Gate 0.4.21: at the device's text size 1.3, "8:27 AM", "10:27 AM" and the now line's "3:35 PM" stood on two lines.
// The column grows with the text size and is a least width, so a time takes what its text needs and never wraps.
describe("the time column", () => {
  test("grows with the device's text size: 1.0, 1.3, 1.5", () => {
    expect(timeColumnWidth("10:27 AM", 41, 1)).toBe(61);
    expect(timeColumnWidth("10:27 AM", 41, 1.3)).toBe(80);
    expect(timeColumnWidth("10:27 AM", 41, 1.5)).toBe(92);
    expect(timeColumnWidth("10:27", 41, 1.3)).toBe(54);
  });
  test("never narrower than its own size when the text is smaller", () => {
    expect(timeColumnWidth("10:27 AM", 41, 0.85)).toBe(61);
  });
});
