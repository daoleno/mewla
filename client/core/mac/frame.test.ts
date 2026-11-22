import { describe, expect, test } from "bun:test";
import { lightsLead } from "./frame";

describe("the traffic lights' room in a one-column header", () => {
  test("one column with the hidden title bar: after the lights; two panes, full screen and off the Mac: none", () => {
    // The shipped app's walk (11-22): Calendar in the 720 first-run window drew ‹ and its title under the lights.
    expect(lightsLead(false, 46)).toBe(84);
    expect(lightsLead(true, 46)).toBe(0);
    expect(lightsLead(false, 0)).toBe(0);
  });
});
