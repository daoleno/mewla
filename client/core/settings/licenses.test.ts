import { describe, expect, test } from "bun:test";
import data from "../../ui/more/settings/licenses/licenses.json";
import { findLicenseItem, licenseItemId, licenseSummary, type LicenseData, type LicenseItem } from "./licenses";

describe("open-source licenses", () => {
  const licenses = data as LicenseData;
  const all = (items: LicenseItem[]): LicenseItem[] => items.flatMap((item) => [item, ...all(item.includes ?? [])]);
  const items = licenses.sections.flatMap((section) => all(section.items));

  test("names the works the app must credit", () => {
    const names = new Set(items.map((item) => item.name));
    for (const name of ["Native Shell (NSH)", "Ghostty (libghostty-vt)", "noVNC", "TweetNaCl.js"]) expect(names.has(name)).toBe(true);
    expect(items.find((item) => item.name === "noVNC")?.license).toBe("MPL-2.0");
  });

  test("every item has a license text or explains where its terms are", () => {
    for (const item of items) {
      if (item.text) expect(licenses.texts[item.text]?.length ?? 0).toBeGreaterThan(20);
      else expect(item.note ?? "").toContain("https://");
      expect(item.license).not.toBe("UNKNOWN");
    }
  });

  test("finds items by id, including packages bundled into another", () => {
    const sample: LicenseData = { sections: [{ title: "s", items: [{ name: "a", license: "MIT", includes: [{ name: "b", license: "ISC" }] }] }], texts: {} };
    expect(findLicenseItem(sample, licenseItemId(0, 0))?.name).toBe("a");
    expect(findLicenseItem(sample, licenseItemId(0, 0, 0))?.name).toBe("b");
    expect(findLicenseItem(sample, "x")).toBeNull();
    expect(findLicenseItem(sample, "999/0")).toBeNull();
  });

  test("summarizes version, license and platform", () => {
    expect(licenseSummary({ name: "a", version: "1.0.0", license: "MIT" })).toBe("1.0.0 · MIT");
    expect(licenseSummary({ name: "b", license: "BSD-3-Clause", platforms: ["ios"] })).toBe("BSD-3-Clause · iOS only");
    expect(licenseSummary({ name: "c", license: "MIT", includes: [{ name: "d", license: "ISC" }] })).toBe("MIT · includes 1 package");
  });
});
