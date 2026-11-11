import { describe, expect, test } from "bun:test";
import { settingsLinkRoutes } from "./settingsLink";

describe("settings links", () => {
  test("mewla://settings opens Settings", () => {
    expect(settingsLinkRoutes("mewla://settings")).toEqual([{ name: "settings" }]);
    expect(settingsLinkRoutes("mewla://settings/")).toEqual([{ name: "settings" }]);
  });

  test("a page under Settings opens over it, as the Expo app's routes did", () => {
    expect(settingsLinkRoutes("mewla://settings/model-providers")).toEqual([{ name: "settings" }, { name: "settings-providers" }]);
    expect(settingsLinkRoutes("mewla://settings/computer?id=s1")).toEqual([{ name: "settings" }, { name: "settings-computer", id: "s1" }]);
    expect(settingsLinkRoutes("mewla://settings/license?id=0%2F1")).toEqual([{ name: "settings" }, { name: "settings-licenses" }, { name: "settings-license", id: "0/1" }]);
    expect(settingsLinkRoutes("mewla://settings/nowhere")).toEqual([{ name: "settings" }]);
  });

  test("addServer opens pairing over Settings; a pairing link is not a page", () => {
    expect(settingsLinkRoutes("mewla://settings?addServer=1")).toEqual([{ name: "settings" }, { name: "pair" }]);
    expect(settingsLinkRoutes("mewla://settings?p=abc")).toBeNull();
    expect(settingsLinkRoutes("mewla://brain")).toBeNull();
    expect(settingsLinkRoutes("mewla://settingsx")).toBeNull();
  });
});
