import { describe, expect, test } from "bun:test";
import {
  catCacheKey,
  catFocusScript,
  catSocketMessage,
  catStateScript,
  filesToFetch,
  isSafeCatPath,
  parseCatBridgeMessage,
  parseCatManifest,
  parseCatView,
  staleVersions,
  type CatView,
} from "./catPark";

const sha = "a".repeat(64);
const manifest = (files: unknown[], version = "0123456789abcdef") => ({ version, files });

describe("manifest", () => {
  test("accepts a well-formed bundle", () => {
    const parsed = parseCatManifest(manifest([{ path: "index.html", size: 10, sha256: sha }, { path: "a/pc_studio.jpg", size: 5, sha256: sha }]));
    expect(parsed?.files.map((file) => file.path)).toEqual(["index.html", "a/pc_studio.jpg"]);
  });

  test("refuses paths that could leave the cache directory", () => {
    for (const path of ["../x", "/etc/passwd", "a/../../x", "a//b", "a/./b", "..", "a\\b", "", "a/b/", ".hidden"]) {
      expect(isSafeCatPath(path)).toBe(false);
      expect(parseCatManifest(manifest([{ path: "index.html", size: 1, sha256: sha }, { path, size: 1, sha256: sha }]))).toBeNull();
    }
  });

  test("refuses a bundle without a page, with duplicates or with bad digests and sizes", () => {
    expect(parseCatManifest(manifest([{ path: "game.js", size: 1, sha256: sha }]))).toBeNull();
    expect(parseCatManifest(manifest([{ path: "index.html", size: 1, sha256: sha }, { path: "index.html", size: 1, sha256: sha }]))).toBeNull();
    expect(parseCatManifest(manifest([{ path: "index.html", size: 1, sha256: "zz" }]))).toBeNull();
    expect(parseCatManifest(manifest([{ path: "index.html", size: -1, sha256: sha }]))).toBeNull();
    expect(parseCatManifest(manifest([{ path: "index.html", size: 1e9, sha256: sha }]))).toBeNull();
    expect(parseCatManifest(manifest([{ path: "index.html", size: 1, sha256: sha }], "../v"))).toBeNull();
    expect(parseCatManifest(null)).toBeNull();
  });

  test("fetches only what the cache lacks or holds at the wrong size", () => {
    const parsed = parseCatManifest(manifest([
      { path: "index.html", size: 10, sha256: sha },
      { path: "game.js", size: 20, sha256: sha },
      { path: "a/x.jpg", size: 30, sha256: sha },
    ]))!;
    const cached = new Map([["index.html", 10], ["game.js", 19]]);
    expect(filesToFetch(parsed, cached).map((file) => file.path)).toEqual(["game.js", "a/x.jpg"]);
    expect(filesToFetch(parsed, new Map(parsed.files.map((file) => [file.path, file.size])))).toEqual([]);
  });

  test("keeps the version in use and the most recent ones", () => {
    expect(staleVersions(["a", "b", "c", "d"], "d", ["c", "b", "a"], 2)).toEqual(["a"]);
    expect(staleVersions(["a"], "a", [], 2)).toEqual([]);
  });
});

describe("bridge", () => {
  test("accepts the page's actions and nothing else", () => {
    expect(parseCatBridgeMessage('{"cat":1,"type":"ready"}')).toEqual({ type: "ready" });
    expect(parseCatBridgeMessage('{"cat":1,"type":"open_brain"}')).toEqual({ type: "open_brain" });
    expect(parseCatBridgeMessage('{"cat":1,"type":"act","act":"feed","arg":"fish"}')).toEqual({ type: "act", act: "feed", arg: "fish" });
    expect(parseCatBridgeMessage('{"cat":1,"type":"act","act":"pack","arg":""}')).toEqual({ type: "act", act: "pack", arg: "" });
    expect(parseCatBridgeMessage('{"cat":1,"type":"act","act":"seen","arg":"postcard:pc:w1:start"}')).toEqual({ type: "act", act: "seen", arg: "postcard:pc:w1:start" });
    for (const bad of [
      "not json", "null", "[]", '{"type":"ready"}', '{"cat":1,"type":"eval","code":"x"}',
      '{"cat":1,"type":"collect","slot":1}', '{"cat":1,"type":"act","act":"feed"}', '{"cat":1,"type":"act","act":"Feed","arg":""}',
      '{"cat":1,"type":"act","act":"pack","arg":"../x"}', '{"cat":1,"type":"act","act":"pack","arg":"a b"}', '{"cat":1,"type":"act","act":7,"arg":""}',
      `{"cat":1,"type":"act","act":"seen","arg":"${"x".repeat(201)}"}`,
      "x".repeat(5000),
    ]) {
      expect(parseCatBridgeMessage(bad)).toBeNull();
    }
  });

  test("only the actions that change state reach the daemon", () => {
    expect(catSocketMessage({ type: "act", act: "pet", arg: "" })).toEqual({ type: "cat_act", act: "pet", arg: "" });
    expect(catSocketMessage({ type: "act", act: "seen", arg: "souvenir:sv:w" })).toEqual({ type: "cat_act", act: "seen", arg: "souvenir:sv:w" });
    expect(catSocketMessage({ type: "back" })).toBeNull();
    expect(catSocketMessage({ type: "open_brain" })).toBeNull();
    expect(catSocketMessage({ type: "ready" })).toBeNull();
  });

  test("the state script is one safe call even with hostile text in a title", () => {
    const view: CatView = {
      presence: { away: true, waiting: false },
      postcards: [], yard: { day: "d", treats: [], pantry: {} }, now: 1,
      cat: { level: 1, stars: 0, need: 6, hobbies: [], pending: [], mood: "calm", today: {}, learned: {}, gap: 0, visits: 1, memory: [] },
      souvenirs: [{ id: "sv:1", work_id: "1", kind: "trail", item: "pinecone", title: "x" + String.fromCharCode(0x2028) + "</script>');alert(1);//", at: 1 }],
    };
    const script = catStateScript(view, "zh-CN");
    expect(script.startsWith("window.__catHost&&window.__catHost(")).toBe(true);
    expect(script.endsWith(");true;")).toBe(true);
    expect(script.includes(String.fromCharCode(0x2028))).toBe(false);
    expect(JSON.parse(catStateScript(view, "en", true).slice("window.__catHost&&window.__catHost(".length, -");true;".length)).tab).toBe(true);
    const literal = script.slice("window.__catHost&&window.__catHost(".length, -");true;".length);
    expect(JSON.parse(literal).state.souvenirs[0].title).toContain("alert(1)");
  });
});

describe("focus script", () => {
  test("tells the page whether it is on screen", () => {
    expect(catFocusScript(false)).toBe('window.__catHost&&window.__catHost({"cat":1,"type":"focus","on":false});true;');
    expect(catFocusScript(true)).toContain('"on":true');
  });
});

describe("cached state", () => {
  test("is kept per computer", () => {
    expect(catCacheKey("a")).not.toBe(catCacheKey("b"));
  });
  test("a cached document must look like a cat view", () => {
    expect(parseCatView({ presence: {}, postcards: [], souvenirs: [], yard: {}, cat: {} })).not.toBeNull();
    expect(parseCatView({ presence: {}, postcards: [], souvenirs: [], yard: {} })).toBeNull();
    expect(parseCatView({ presence: {} })).toBeNull();
    expect(parseCatView("x")).toBeNull();
  });
});
