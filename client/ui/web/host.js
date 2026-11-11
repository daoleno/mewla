// The browser's host for the web build (nsh.json `web.host`): what the page has and the app's
// QuickJS runtime does not. Plain browser JavaScript, loaded by the page `nsh package --target web`
// writes; `createHost(player)` runs before the app starts and answers its `host.call`s.
//
// - secure.*: the device key, in localStorage under the keys the Expo web app used
//   (`mewla:secure:<key>`), so a browser paired before the switch stays paired.
// - app.legacyStorage / app.legacyStorageCopied: the Expo web app's other entries (its AsyncStorage
//   wrote them to localStorage as they are: computers, drafts, preferences), copied once.
// - app.origin: the page's origin, the computer's address for a browser that asks to join.
// - links.initial: the pairing link `mewla pair` gives browsers (`<origin>/#pair=<link>`), read once
//   and taken out of the address bar and history.

const SECURE_PREFIX = "mewla:secure:";
const LEGACY_COPIED = "mewla:web-host:legacy-copied";

function storage() {
  try {
    return window.localStorage;
  } catch {
    // Storage off (some private windows): the app keeps working for this page's life.
    return null;
  }
}

function keyOf(args) {
  const key = args && typeof args.key === "string" ? args.key : "";
  if (!key) throw new Error("secure: key is required");
  return SECURE_PREFIX + key;
}

function legacyEntries() {
  const local = storage();
  if (!local || local.getItem(LEGACY_COPIED) === "1") return null;
  const entries = {};
  for (let index = 0; index < local.length; index++) {
    const key = local.key(index);
    if (!key || !key.startsWith("mewla") || key.startsWith(SECURE_PREFIX) || key === LEGACY_COPIED) continue;
    const value = local.getItem(key);
    if (value !== null) entries[key] = value;
  }
  return Object.keys(entries).length ? entries : null;
}

function initialLink() {
  const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ""));
  if (!fragment.has("pair")) return null;
  const link = (fragment.get("pair") || "").trim();
  window.history.replaceState(window.history.state, "", window.location.pathname + window.location.search);
  return link || null;
}

export function createHost() {
  const handlers = {
    "secure.get": (args) => storage()?.getItem(keyOf(args)) ?? null,
    "secure.set": (args) => {
      storage()?.setItem(keyOf(args), String(args.value ?? ""));
      return null;
    },
    "secure.remove": (args) => {
      storage()?.removeItem(keyOf(args));
      return null;
    },
    "app.legacyStorage": () => legacyEntries(),
    "app.legacyStorageCopied": () => {
      storage()?.setItem(LEGACY_COPIED, "1");
      return null;
    },
    "app.origin": () => window.location.origin,
    "links.initial": () => initialLink(),
  };
  return {
    call(name, args) {
      const handler = handlers[name];
      if (!handler) throw new Error(`${name} is not available in the browser`);
      return handler(args ?? {});
    },
  };
}
