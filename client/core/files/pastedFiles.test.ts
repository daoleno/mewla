import { describe, expect, test } from "bun:test";
import { pastedFileName } from "./pastedFiles";

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
