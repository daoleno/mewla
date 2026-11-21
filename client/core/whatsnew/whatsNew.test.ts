import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  highlightIdsUpTo,
  newsIds,
  latestRelease,
  mayOffer,
  mergeReleases,
  newestOf,
  type OfferGate,
  parseRecord,
  parseReleases,
  pendingPages,
  releasesFor,
  updateLines,
  type WhatsNewRecord,
  type WhatsNewRelease,
} from "./whatsNew";

function release(version: string, names: string[], fixes: string[] = []): WhatsNewRelease {
  return {
    version,
    date: "2026-11-16",
    highlights: names.map((name) => ({ id: `${version}-${name}`, title: name, line: name, media: { scene: "chat-card" } })),
    fixes,
  };
}

const record = (over: Partial<WhatsNewRecord> = {}): WhatsNewRecord => ({ baseline: "0.4.6", seen: [], updates: [], ...over });

describe("pendingPages", () => {
  const notes = [release("0.4.7", ["a", "b", "c"]), release("0.4.8", ["d", "e"]), release("0.4.9", ["f", "g", "h"])];

  test("a fixes-only release gives no page", () => {
    expect(pendingPages([release("0.4.9", [], ["A fix."])], record({ baseline: "0.4.8" }), "0.4.9")).toEqual([]);
  });

  test("three missed releases are one sheet of at most three pages, each release's headline first, newest first", () => {
    const pages = pendingPages(notes, record(), "0.4.9");
    expect(pages.map((p) => p.id)).toEqual(["0.4.9-f", "0.4.8-d", "0.4.7-a"]);
  });

  test("one missed release shows its own first three", () => {
    expect(pendingPages(notes, record({ baseline: "0.4.8" }), "0.4.9").map((p) => p.id)).toEqual(["0.4.9-f", "0.4.9-g", "0.4.9-h"]);
  });

  test("what was seen anywhere (the phone or the web) is not shown again", () => {
    const pages = pendingPages(notes, record({ baseline: "0.4.8", seen: ["0.4.9-f", "0.4.9-g"] }), "0.4.9");
    expect(pages.map((p) => p.id)).toEqual(["0.4.9-h"]);
  });

  test("nothing at or below the installed version, nothing newer than the Mewla in use", () => {
    expect(pendingPages(notes, record({ baseline: "0.4.9" }), "0.4.9")).toEqual([]);
    expect(pendingPages(notes, record({ baseline: "0.4.7" }), "0.4.8").map((p) => p.id)).toEqual(["0.4.8-d", "0.4.8-e"]);
  });

  test("a Mewla from before the record counts every release with notes", () => {
    expect(pendingPages(notes, record({ baseline: "" }), "0.4.8").map((p) => p.id)).toEqual(["0.4.8-d", "0.4.7-a", "0.4.8-e"]);
  });

  test("one sheet settles every release it covers: the highlights it left out are not offered after it", () => {
    const r = record();
    const ids = newsIds(notes, r, "0.4.9");
    expect(ids.length).toBe(8);
    expect(pendingPages(notes, { ...r, seen: ids }, "0.4.9")).toEqual([]);
  });

  test("the pages carry their release's version for the small label", () => {
    expect(pendingPages(notes, record({ baseline: "0.4.8" }), "0.4.9")[0].version).toBe("0.4.9");
  });
});

describe("the update that counts", () => {
  test("the app's own update and the computer's both count, as one sheet up to the newer", () => {
    expect(newestOf("0.4.9", "0.4.8")).toBe("0.4.9");
    expect(newestOf(null, "0.4.8")).toBe("0.4.8");
    expect(newestOf("dev", undefined)).toBeNull();
  });

  test("a fresh install marks every highlight up to its version seen", () => {
    const notes = [release("0.4.8", ["d"]), release("0.4.9", ["f"]), release("0.5.0", ["z"])];
    expect(highlightIdsUpTo(notes, "0.4.9").sort()).toEqual(["0.4.8-d", "0.4.9-f"]);
  });
});

describe("notes", () => {
  test("the computer's notes fill in releases newer than the app; the app's own copy wins", () => {
    const own = [release("0.4.8", ["mine"])];
    const computer = [release("0.4.8", ["theirs"]), release("0.4.9", ["new"])];
    const merged = mergeReleases(own, computer);
    expect(merged.map((r) => r.version)).toEqual(["0.4.9", "0.4.8"]);
    expect(merged[1].highlights[0].id).toBe("0.4.8-mine");
  });

  test("a malformed bundle or record reads leniently", () => {
    expect(parseReleases({ releases: [{ version: "x" }, { version: "0.4.9", highlights: [{ id: 1 }], fixes: ["ok", 2] }] }))
      .toEqual([{ version: "0.4.9", date: "", highlights: [], fixes: ["ok"] }]);
    expect(parseRecord(null)).toBeNull();
    expect(parseRecord({ baseline: "0.4.8", seen: ["a", 1], updates: [{ version: "0.4.9", at: 5 }, { version: 2 }] }))
      .toEqual({ baseline: "0.4.8", seen: ["a"], updates: [{ version: "0.4.9", at: 5 }] });
  });

  test("the app's bundled notes parse whole, with a Try it for most pages", () => {
    const raw = JSON.parse(readFileSync(join(import.meta.dir, "../../ui/whatsnew/notes.json"), "utf8"));
    const releases = parseReleases(raw);
    expect(releases.length).toBe(raw.releases.length);
    for (const r of releases) expect(r.highlights.length).toBe(raw.releases.find((x: { version: string }) => x.version === r.version).highlights.length);
    expect(latestRelease(releases, "0.4.8")?.version).toBe("0.4.8");
  });
});

describe("releasesFor", () => {
  const notes = parseReleases({
    releases: [{
      version: "0.4.20", date: "2026-11-22", fixes: [],
      highlights: [
        { id: "0.4.20-share", title: "Share", line: "Share.", media: { scene: "share-in" }, platforms: ["android"] },
        { id: "0.4.20-git", title: "Git", line: "Git.", media: { scene: "git-button" } },
      ],
    }],
  });

  test("a highlight of one platform is offered only there, and only there does a sheet settle it", () => {
    const os = (name: string) => releasesFor(notes, name);
    expect(pendingPages(os("android"), record(), "0.4.20").map((p) => p.id)).toEqual(["0.4.20-share", "0.4.20-git"]);
    expect(pendingPages(os("ios"), record(), "0.4.20").map((p) => p.id)).toEqual(["0.4.20-git"]);
    expect(newsIds(os("web"), record(), "0.4.20")).toEqual(["0.4.20-git"]);
    // Seen on the web, the phone still offers what only it has.
    expect(pendingPages(os("android"), record({ seen: ["0.4.20-git"] }), "0.4.20").map((p) => p.id)).toEqual(["0.4.20-share"]);
  });

  test("a release whose highlights are all another platform's has no page here, and its line opens nothing it cannot show", () => {
    const only = releasesFor(parseReleases({ releases: [{ version: "0.4.20", date: "2026-11-22", fixes: [], highlights: [{ id: "0.4.20-share", title: "Share", line: "Share.", media: { scene: "share-in" }, platforms: ["android"] }] }] }), "ios");
    expect(pendingPages(only, record(), "0.4.20")).toEqual([]);
    expect(updateLines(record({ updates: [{ version: "0.4.20", at: 1 }] }), only)[0]).toMatchObject({ highlights: false, notes: false });
  });
});

describe("updateLines", () => {
  test("one line per version, in time order, saying whether it has pages and whether it has notes at all", () => {
    const notes = [{ ...release("0.4.8", []), fixes: ["A fix."] }, release("0.4.9", ["f"])];
    const updates = [{ version: "0.4.9", at: 20 }, { version: "0.4.8", at: 10 }, { version: "0.4.9", at: 30 }, { version: "0.4.10", at: 40 }];
    const lines = updateLines(record({ updates }), notes);
    expect(lines).toEqual([
      { version: "0.4.8", at: 10, highlights: false, notes: true },
      { version: "0.4.9", at: 20, highlights: true, notes: true },
      // Released without notes: its line has nothing to open.
      { version: "0.4.10", at: 40, highlights: false, notes: false },
    ]);
  });
});

describe("mayOffer", () => {
  const quiet: OfferGate = {
    onBrainHome: true,
    active: true,
    settingUp: false,
    openedForSomething: false,
    composing: false,
    overlayOpen: false,
    needsPerson: false,
  };

  test("only on a quiet Brain home", () => {
    expect(mayOffer(quiet)).toBe(true);
    for (const key of ["settingUp", "openedForSomething", "composing", "overlayOpen", "needsPerson"] as const) {
      expect(mayOffer({ ...quiet, [key]: true })).toBe(false);
    }
    expect(mayOffer({ ...quiet, onBrainHome: false })).toBe(false);
    expect(mayOffer({ ...quiet, active: false })).toBe(false);
  });
});
