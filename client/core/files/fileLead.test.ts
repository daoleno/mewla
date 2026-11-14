import { describe, expect, test } from "bun:test";
import { fileLead } from "./fileLead";

describe("a file row's leading icon (SPEC §10.4)", () => {
  test("a folder is the folder glyph on the neutral tile", () => {
    expect(fileLead("directory", "worklog")).toEqual({ kind: "tile", glyph: "folder" });
  });

  test("a file is its kind's glyph on the neutral tile", () => {
    expect(fileLead("file", "notes/README.md")).toEqual({ kind: "tile", glyph: "document-text" });
    expect(fileLead("file", "build.log")).toEqual({ kind: "tile", glyph: "document" });
  });

  test("a file whose name names its product gets that product's mark", () => {
    expect(fileLead("file", "web/package.json")).toEqual({ kind: "mark", mark: "nodejs" });
    expect(fileLead("file", "go.mod")).toEqual({ kind: "mark", mark: "go" });
    expect(fileLead("file", "deploy/Dockerfile")).toEqual({ kind: "mark", mark: "docker" });
    // A folder named like one stays a folder.
    expect(fileLead("directory", "package.json")).toEqual({ kind: "tile", glyph: "folder" });
  });
});
