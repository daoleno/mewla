import { describe, expect, test } from "bun:test";
import { localFilePath, pastedFileName, pasteAttaches, withoutInserted } from "./pastedFiles";

describe("pastedFileName", () => {
  const at = new Date(2026, 10, 11, 8, 5, 9);

  test("a clipboard picture is named by when it was pasted", () => {
    expect(pastedFileName("image.png", "image/png", at)).toBe("Pasted image 2026-11-11 08.05.09.png");
    expect(pastedFileName("", "image/jpeg", at)).toBe("Pasted image 2026-11-11 08.05.09.jpg");
  });

  test("a nameless file of another type says file, with its extension when known", () => {
    expect(pastedFileName("", "application/pdf", at)).toBe("Pasted file 2026-11-11 08.05.09.pdf");
    expect(pastedFileName("", "application/x-unknown", at)).toBe("Pasted file 2026-11-11 08.05.09");
  });

  test("a real name is kept", () => {
    expect(pastedFileName("報告 1.pdf", "application/pdf", at)).toBe("報告 1.pdf");
    expect(pastedFileName("image.backup.png", "image/png", at)).toBe("image.backup.png");
  });
});

describe("pasteAttaches", () => {
  const paste = (over: Partial<Parameters<typeof pasteAttaches>[0]>) => pasteAttaches({ text: "", html: "", files: 0, uris: [], ...over });

  test("a screenshot to the clipboard becomes a chip", () => {
    expect(paste({ files: 1 })).toEqual({ files: true, uris: false });
  });

  test("a picture copied from a page, its address beside it, becomes a chip", () => {
    expect(paste({ files: 1, html: '<img src="https://example.com/cat.png">' })).toEqual({ files: true, uris: false });
    expect(paste({ files: 1, text: "https://example.com/cat.png", html: "<img>" })).toEqual({ files: true, uris: false });
  });

  test("a spreadsheet cell or a page's selection stays text: its picture of itself is not attached", () => {
    expect(paste({ files: 1, text: "Q3 revenue\t4,210", html: "<table><tr><td>Q3 revenue</td></tr></table>" })).toEqual({ files: false, uris: false });
  });

  test("plain text with a picture keeps both", () => {
    expect(paste({ files: 1, text: "see this" })).toEqual({ files: true, uris: false });
  });

  test("files copied in a file manager become chips, not their icons", () => {
    expect(paste({ text: "report.pdf", uris: ["file:///Users/me/report.pdf"], files: 1 })).toEqual({ files: false, uris: true });
  });

  test("text alone attaches nothing", () => {
    expect(paste({ text: "npm run build" })).toEqual({ files: false, uris: false });
  });
});

describe("localFilePath", () => {
  test("a file URL is its decoded path", () => {
    expect(localFilePath("file:///Users/me/Screen%20Shot%202026-11-23.png")).toBe("/Users/me/Screen Shot 2026-11-23.png");
    expect(localFilePath("file://localhost/tmp/a.png")).toBe("/tmp/a.png");
    expect(localFilePath("file:///Users/me/%E5%9B%BE.png")).toBe("/Users/me/图.png");
  });

  test("anything else is no local file", () => {
    expect(localFilePath("https://example.com/a.png")).toBeNull();
    expect(localFilePath("file://server/share/a.png")).toBeNull();
    expect(localFilePath("file:///bad%E5")).toBeNull();
  });
});

describe("withoutInserted", () => {
  test("takes a pasted name back out where it went in, the caret there", () => {
    expect(withoutInserted("look at ", "look at report.pdf", "report.pdf")).toEqual({ value: "look at ", caret: 8 });
    expect(withoutInserted("ab", "areport.pdfb", "report.pdf")).toEqual({ value: "ab", caret: 1 });
    expect(withoutInserted("", "a.png\nb.png", "a.png\nb.png")).toEqual({ value: "", caret: 0 });
    expect(withoutInserted("a", "aa", "a")).toEqual({ value: "a", caret: 1 });
    expect(withoutInserted("ab", "abxb", "bx")).toEqual({ value: "ab", caret: 1 });
  });

  test("leaves a field the person changed otherwise", () => {
    expect(withoutInserted("abc", "report.pdf", "report.pdf")).toBeNull();
    expect(withoutInserted("ab", "ab report.pdf!", "report.pdf")).toBeNull();
    expect(withoutInserted("ab", "ab", "")).toBeNull();
  });
});
