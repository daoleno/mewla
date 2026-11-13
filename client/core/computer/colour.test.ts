import { describe, expect, test } from "bun:test";
import { connectionTone, fixRowLead, fixRowTone } from "./colour";

describe("the computer page's colour (SPEC §10.4)", () => {
  test("a Needs fixing row is its thing's group tile", () => {
    expect(fixRowLead({ key: "pair" })).toEqual({ kind: "tile", glyph: "link" });
    expect(fixRowLead({ key: "connection" })).toEqual({ kind: "tile", glyph: "cloud-offline" });
    expect(fixRowLead({ key: "host" })).toEqual({ kind: "tile", glyph: "brain" });
    expect(fixRowLead({ key: "browser" })).toEqual({ kind: "mark", mark: "chrome" });
    expect(fixRowLead({ key: "update-optional" })).toEqual({ kind: "tile", glyph: "cloud-download" });
  });

  test("its words: red only where the computer cannot work for this phone, amber where a fix waits", () => {
    expect(fixRowTone({ key: "pair", blocking: true, button: "Pair" })).toBe("error");
    expect(fixRowTone({ key: "update", blocking: true, button: "Update" })).toBe("error");
    expect(fixRowTone({ key: "browser", blocking: false, button: "Fix" })).toBe("warn");
    expect(fixRowTone({ key: "update", blocking: false })).toBe("off");
    expect(fixRowTone({ key: "update-optional", blocking: false, button: "Update" })).toBe("off");
  });

  test("the header: connected ok, this computer offline red, connecting and not connected soft", () => {
    expect(connectionTone("connected")).toBe("ok");
    expect(connectionTone("waiting")).toBe("error");
    expect(connectionTone("connecting")).toBe("off");
    expect(connectionTone("idle")).toBe("off");
  });
});
