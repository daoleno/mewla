/**
 * The More pages a `mewla://settings…` link opens, as the Expo app's routes did:
 * `mewla://settings`, `…/settings/computer?id=…`, `…/settings/model-providers`, and so
 * on; `addServer` opens pairing over Settings. A pairing link (`?p=`) is not one of
 * these: it pairs. Null for any other link.
 */
export type SettingsLinkRoute =
  | { name: "settings" }
  | { name: "settings-computer"; id: string }
  | { name: "settings-providers" }
  | { name: "settings-agents" }
  | { name: "settings-friends" }
  | { name: "settings-phone" }
  | { name: "settings-iphone" }
  | { name: "settings-virtual-screen" }
  | { name: "settings-pet" }
  | { name: "settings-licenses" }
  | { name: "settings-license"; id: string }
  | { name: "pair" };

const PAGES: Record<string, SettingsLinkRoute> = {
  "model-providers": { name: "settings-providers" },
  agents: { name: "settings-agents" },
  friends: { name: "settings-friends" },
  phone: { name: "settings-phone" },
  iphone: { name: "settings-iphone" },
  "virtual-screen": { name: "settings-virtual-screen" },
  pet: { name: "settings-pet" },
  licenses: { name: "settings-licenses" },
};

export function settingsLinkRoutes(url: string): SettingsLinkRoute[] | null {
  const match = /^mewla:\/\/settings(?:\/([a-z-]+))?\/?(?:\?([^#]*))?(?:#.*)?$/i.exec(url.trim());
  if (!match) return null;
  const params = new URLSearchParams(match[2] ?? "");
  if (params.has("p")) return null;
  const page = (match[1] ?? "").toLowerCase();
  const routes: SettingsLinkRoute[] = [{ name: "settings" }];
  if (page === "computer") {
    const id = params.get("id");
    if (id) routes.push({ name: "settings-computer", id });
  } else if (page === "license") {
    const id = params.get("id");
    if (id) routes.push({ name: "settings-licenses" }, { name: "settings-license", id });
  } else if (page && PAGES[page]) {
    routes.push(PAGES[page]);
  } else if (page) {
    return routes;
  }
  if (!page && params.has("addServer")) routes.push({ name: "pair" });
  return routes;
}
