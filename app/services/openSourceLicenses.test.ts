import { describe, expect, test } from "bun:test";
import {
  findLicenseItem,
  licenseItemId,
  licenseSummary,
  openSourceLicenses,
  type LicenseItem,
} from "./openSourceLicenses";

describe("open-source licenses", () => {
  const data = openSourceLicenses();
  const all = (items: LicenseItem[]): LicenseItem[] =>
    items.flatMap((item) => [item, ...all(item.includes ?? [])]);
  const items = data.sections.flatMap((section) => all(section.items));

  test("names the works the app must credit", () => {
    const names = new Set(items.map((item) => item.name));
    for (const name of [
      "Ghostty (libghostty-vt)",
      "noVNC",
      "Mermaid",
      "Inter",
      "Bricolage Grotesque",
      "Maple Mono",
      "Hermes",
      "SDWebImage",
      "react-native",
      "expo",
    ]) {
      expect(names.has(name)).toBe(true);
    }
    expect(items.find((item) => item.name === "noVNC")?.license).toBe("MPL-2.0");
    expect(items.find((item) => item.name === "Inter")?.license).toBe("OFL-1.1");
  });

  test("every item has a license text or explains where its terms are", () => {
    for (const item of items) {
      if (item.text) expect(data.texts[item.text]?.length ?? 0).toBeGreaterThan(20);
      else expect(item.note ?? "").toContain("https://");
      expect(item.license).not.toBe("UNKNOWN");
    }
  });

  test("finds items by id, including packages bundled into another", () => {
    const sectionIndex = data.sections.findIndex((section) => section.items.some((item) => item.includes?.length));
    const itemIndex = data.sections[sectionIndex].items.findIndex((item) => item.includes?.length);
    const parent = data.sections[sectionIndex].items[itemIndex];
    expect(findLicenseItem(data, licenseItemId(sectionIndex, itemIndex))).toBe(parent);
    expect(findLicenseItem(data, licenseItemId(sectionIndex, itemIndex, 0))).toBe(parent.includes![0]);
    expect(findLicenseItem(data, "x")).toBeNull();
    expect(findLicenseItem(data, "999/0")).toBeNull();
  });

  test("summarizes version, license and platform", () => {
    expect(licenseSummary({ name: "a", version: "1.0.0", license: "MIT" })).toBe("1.0.0 · MIT");
    expect(licenseSummary({ name: "b", license: "BSD-3-Clause", platforms: ["ios"] })).toBe("BSD-3-Clause · iOS only");
    expect(
      licenseSummary({ name: "c", license: "MIT", includes: [{ name: "d", license: "ISC" }] }),
    ).toBe("MIT · includes 1 package");
  });
});
