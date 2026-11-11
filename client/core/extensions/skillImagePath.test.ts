import { expect, test } from "bun:test";
import { skillFileLink, skillImagePath } from "./skillImagePath";

test("a link in a Skill file opens another file of the same copy, and nothing outside it", () => {
  const files = [{ path: "SKILL.md" }, { path: "docs/setup.md" }, { path: "scripts/run.sh" }];
  expect(skillFileLink("docs/setup.md#usage", "SKILL.md", files)).toBe("docs/setup.md");
  expect(skillFileLink("./docs/setup.md", "SKILL.md", files)).toBe("docs/setup.md");
  expect(skillFileLink("../scripts/run.sh", "docs/setup.md", files)).toBe("scripts/run.sh");
  for (const url of ["#usage", "https://example.com/docs", "/etc/passwd", "../../outside.md", "missing.md", ""]) {
    expect(skillFileLink(url, "SKILL.md", files)).toBeNull();
  }
});

test("Markdown image resolution stays in the exact advertised Skill copy", () => {
  const files = [{ path: "images/photo one.png" }, { path: "docs/screen.png" }];
  expect(skillImagePath("../images/photo%20one.png", "docs/guide.md", files)).toBe("images/photo one.png");
  expect(skillImagePath("screen.png", "docs/guide.md", files)).toBe("docs/screen.png");
  expect(skillImagePath("skill-file:images/photo one.png", "docs/guide.md", files)).toBe("images/photo one.png");
  for (const path of ["../../outside.png", "/private/photo.png", "missing.png", "file:///private/photo.png"]) {
    expect(() => skillImagePath(path, "docs/guide.md", files)).toThrow();
  }
});
