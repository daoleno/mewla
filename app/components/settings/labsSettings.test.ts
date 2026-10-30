import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";

const read = (path: string) => readFileSync(new URL(path, import.meta.url), "utf8");
const settings = read("../../app/settings.tsx");
const labs = read("./LabsSection.tsx");

describe("Settings → Labs", () => {
  test("Labs sits above Calendar", () => {
    const labsAt = settings.indexOf("<SettingsSectionHeader>Labs</SettingsSectionHeader>");
    const calendarAt = settings.indexOf("<SettingsSectionHeader>Calendar</SettingsSectionHeader>");
    expect(labsAt).toBeGreaterThan(settings.indexOf("<SettingsSectionHeader>Agents</SettingsSectionHeader>"));
    expect(labsAt).toBeLessThan(calendarAt);
  });

  test("one row per feature, each opening its own page, with no switch on the list", () => {
    expect(labs).toContain('router.push("/settings/friends")');
    expect(labs).toContain('router.push("/settings/phone")');
    expect(labs.match(/accessory="chevron"/g)).toHaveLength(2);
    expect(labs).not.toContain("InkSwitch");
    for (const page of ["friends", "phone"]) expect(existsSync(new URL(`../../app/settings/${page}.tsx`, import.meta.url))).toBe(true);
  });

  test("a feature's controls live on its page", () => {
    const friends = read("./FriendsSettings.tsx");
    for (const text of ["Post without asking", "Let it attach pictures and video", "Open Friends", "Turn off Friends?"]) {
      expect(friends).toContain(text);
      expect(labs).not.toContain(text);
    }
    expect(read("./PhoneSettings.tsx")).toContain('accessibilityLabel="Phone use"');
  });
});
