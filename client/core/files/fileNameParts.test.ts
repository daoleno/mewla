import { describe, expect, test } from "bun:test";
import { fileNameParts, unbroken } from "./fileNameParts";

describe("a file name on one line keeps its end (owner 11-17)", () => {
  test("the owner's transcript keeps `(2).md`", () => {
    const name = "20261117185847-转写_美元债蒸馏讨论-逐字稿文本-1 (2).md";
    const parts = fileNameParts(name);
    expect(parts).toEqual({ head: "20261117185847-转写_美元债蒸馏讨论-逐字稿文本-1 ", tail: "(2).md" });
    expect(parts.head + parts.tail).toBe(name);
  });

  test("an English name keeps the last characters and the extension", () => {
    expect(fileNameParts("meeting-transcript-verbatim-final-v3.pdf")).toEqual({ head: "meeting-transcript-verbatim-fina", tail: "l-v3.pdf" });
  });

  test("a double extension falls in the tail", () => {
    expect(fileNameParts("release-artifacts-linux-amd64.tar.gz")).toEqual({ head: "release-artifacts-linux-amd64", tail: ".tar.gz" });
  });

  test("a short name, a name without an extension, a dotfile and an odd extension stay whole", () => {
    expect(fileNameParts("notes.md")).toEqual({ head: "notes.md", tail: "" });
    expect(fileNameParts("a-very-long-name-without-any-extension")).toEqual({ head: "a-very-long-name-without-any-extension", tail: "" });
    expect(fileNameParts(".a-very-long-dotfile-name-here")).toEqual({ head: ".a-very-long-dotfile-name-here", tail: "" });
    expect(fileNameParts("a-very-long-name. with a sentence after the dot")).toEqual({ head: "a-very-long-name. with a sentence after the dot", tail: "" });
  });

  test("characters outside the BMP are never cut in two", () => {
    const parts = fileNameParts("a-long-picture-name-😀😀😀.png");
    expect(parts.tail).toBe("-😀😀😀.png");
    expect(parts.head + parts.tail).toBe("a-long-picture-name-😀😀😀.png");
  });
});

describe("an unbroken run", () => {
  test("joins every character, a pair outside the BMP kept whole", () => {
    expect(unbroken("a-b😀")).toBe("a⁠-⁠b⁠😀");
    expect(unbroken("")).toBe("");
  });
});
