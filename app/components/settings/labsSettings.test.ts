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
    for (const text of ["Post without asking", "Let it attach pictures and video", "Open Friends", "Delete Friends account"]) {
      expect(friends).toContain(text);
      expect(labs).not.toContain(text);
    }
    expect(read("./PhoneSettings.tsx")).toContain('accessibilityLabel="Phone use"');
  });
});

describe("Settings is one visual system", () => {
  const sections = ["AppearanceSection", "AboutSection", "ComputersSection", "CalendarAlertsSection", "LabsSection"];

  test("every section is a ListSection of ListRows, with no control drawn its own way", () => {
    expect(settings).not.toContain("SegmentedControl");
    expect(settings).not.toContain("PetPicker");
    for (const name of sections) {
      const source = read(`./${name}.tsx`);
      expect(source).toContain("<ListSection");
      expect(source).not.toContain("SegmentedControl");
    }
  });

  test("turning Friends off is only the switch; deleting is its own confirmed row", () => {
    const friends = read("./FriendsSettings.tsx");
    expect(friends).toContain("void friends.turnOff()");
    expect(friends).not.toContain("Turn off Friends?");
    expect(friends).not.toContain("Delete and turn off");
    expect(friends).toContain('title="Delete Friends account?"');
    expect(friends).toContain(".deleteAccount()");
    expect(read("../../services/friendsAccount.ts")).toContain('op: "delete_account"');
    expect(read("../../services/friendsAccount.ts")).not.toContain('op: "disable"');
  });

  test("no explanatory footers or filler subtitles", () => {
    for (const name of [...sections, "FriendsSettings", "PhoneSettings"]) expect(read(`./${name}.tsx`)).not.toContain("footer=");
    expect(read("./FriendsSettings.tsx")).not.toContain("A small public timeline");
    expect(read("./CalendarAlertsSection.tsx")).not.toContain("can't miss");
    expect(settings).not.toContain("Talk to Brain from Telegram");
    expect(settings).not.toContain("Official login or your own API keys");
  });
});
