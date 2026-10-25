import { describe, expect, test } from "bun:test";
import {
  catCacheKey,
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
    expect(parseCatBridgeMessage('{"cat":1,"type":"collect","slot":2}')).toEqual({ type: "collect", slot: 2 });
    expect(parseCatBridgeMessage('{"cat":1,"type":"pack","treat":"fish"}')).toEqual({ type: "pack", treat: "fish" });
    expect(parseCatBridgeMessage('{"cat":1,"type":"pack","treat":""}')).toEqual({ type: "pack", treat: "" });
    expect(parseCatBridgeMessage('{"cat":1,"type":"style","style":"watercolor"}')).toEqual({ type: "style", style: "watercolor" });
    expect(parseCatBridgeMessage('{"cat":1,"type":"seen","what":"postcard","id":"pc:w1:start"}')).toEqual({ type: "seen", what: "postcard", id: "pc:w1:start" });
    for (const bad of [
      "not json", "null", "[]", '{"type":"ready"}', '{"cat":1,"type":"eval","code":"x"}',
      '{"cat":1,"type":"collect","slot":-1}', '{"cat":1,"type":"collect","slot":1.5}', '{"cat":1,"type":"collect","slot":"1"}',
      '{"cat":1,"type":"pack","treat":"../x"}', '{"cat":1,"type":"style","style":"../crayon"}', '{"cat":1,"type":"style","style":7}', '{"cat":1,"type":"seen","what":"work","id":"x"}', '{"cat":1,"type":"seen","what":"postcard","id":""}',
      "x".repeat(5000),
    ]) {
      expect(parseCatBridgeMessage(bad)).toBeNull();
    }
  });

  test("only the actions that change state reach the daemon", () => {
    expect(catSocketMessage({ type: "collect", slot: 1 })).toEqual({ type: "cat_collect", slot: 1 });
    expect(catSocketMessage({ type: "pack", treat: "yarn" })).toEqual({ type: "cat_pack", treat: "yarn" });
    expect(catSocketMessage({ type: "style", style: "pixel" })).toEqual({ type: "cat_style", style: "pixel" });
    expect(catSocketMessage({ type: "seen", what: "souvenir", id: "sv:w" })).toEqual({ type: "cat_seen", what: "souvenir", id: "sv:w" });
    expect(catSocketMessage({ type: "back" })).toBeNull();
    expect(catSocketMessage({ type: "open_brain" })).toBeNull();
    expect(catSocketMessage({ type: "ready" })).toBeNull();
  });

  test("the state script is one safe call even with hostile text in a title", () => {
    const view: CatView = {
      presence: { away: true, waiting: false },
      postcards: [], yard: { day: "d", treats: [], pantry: {} }, now: 1,
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

describe("cached state", () => {
  test("is kept per computer", () => {
    expect(catCacheKey("a")).not.toBe(catCacheKey("b"));
  });
  test("a cached document must look like a cat view", () => {
    expect(parseCatView({ presence: {}, postcards: [], souvenirs: [], yard: {} })).not.toBeNull();
    expect(parseCatView({ presence: {} })).toBeNull();
    expect(parseCatView("x")).toBeNull();
  });
});
