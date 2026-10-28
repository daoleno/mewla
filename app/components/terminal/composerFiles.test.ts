import { describe, expect, test } from "bun:test";
import { browserFileAsset } from "../../services/browserFileAsset";
import { clipboardFiles, dragHasFiles } from "./composerFiles";

function file(name: string, type: string) {
  return new globalThis.File(["x"], name, { type });
}

function clipboard(files: File[], text = "") {
  return { files, getData: (kind: string) => (kind === "text/plain" ? text : "") } as unknown as DataTransfer;
}

describe("clipboard files", () => {
  test("a pasted screenshot or file attaches", () => {
    const png = file("image.png", "image/png");
    expect(clipboardFiles(clipboard([png]))).toEqual([png]);
    const pdf = file("plan.pdf", "application/pdf");
    expect(clipboardFiles(clipboard([pdf], "plan.pdf"))).toEqual([pdf]);
  });

  test("a rich cell range pastes its text, not its picture", () => {
    expect(clipboardFiles(clipboard([file("image.png", "image/png")], "a\tb\n1\t2"))).toEqual([]);
  });

  test("plain text and an empty clipboard attach nothing", () => {
    expect(clipboardFiles(clipboard([], "hello"))).toEqual([]);
    expect(clipboardFiles(null)).toEqual([]);
  });
});

describe("drag", () => {
  test("only a drag that carries files is a drop target", () => {
    expect(dragHasFiles({ types: ["Files"] } as unknown as DataTransfer)).toBe(true);
    expect(dragHasFiles({ types: ["text/plain", "text/uri-list"] } as unknown as DataTransfer)).toBe(false);
    expect(dragHasFiles(null)).toBe(false);
  });
});

describe("browser file as an upload asset", () => {
  const now = new Date(2026, 9, 28, 14, 5, 9);

  test("a clipboard image is named by when it was pasted", () => {
    const asset = browserFileAsset(file("image.png", "image/png"), now);
    expect(asset.name).toBe("Pasted image 2026-10-28 14.05.09.png");
    expect(asset.mimeType).toBe("image/png");
    expect(asset.size).toBe(1);
    expect(asset.file).toBeDefined();
  });

  test("a named file keeps its name", () => {
    expect(browserFileAsset(file("notes.md", "text/markdown"), now).name).toBe("notes.md");
  });
});
