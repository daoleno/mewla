import { describe, expect, test } from "bun:test";
import { BESIDE_MIN_WIDTH, besideMode, besideShows, crossBeside, intoBeside, isPreview, openedBeside, opensBeside, outOfBeside, PREVIEW_BESIDE_MIN_WIDTH, previewSplit, shareOf, shownRoutes, storedShare } from "./beside";

const home = { name: "home" };
const session = { name: "session", workerId: "w1" };
const work = { name: "workDetail", workId: "k1" };
const settings = { name: "settings" };

describe("beside", () => {
  test("the board from a 1480 right pane (a 1920 window), the preview split from 1028 (a laptop), never a phone", () => {
    const mode = (rightPane: number, desktop = true, wide = true) => besideMode({ desktop, wide, rightPane });
    expect(PREVIEW_BESIDE_MIN_WIDTH).toBe(1028);
    expect(mode(1920 - 320)).toBe("board");
    expect(mode(1920 - 420)).toBe("board");
    expect(mode(BESIDE_MIN_WIDTH - 1)).toBe("preview");
    // Web with the 320 pane: 1366, 1440; the Mac with its 280: 1440, 1512, 1728.
    expect([1366 - 320, 1440 - 320, 1440 - 280, 1512 - 280, 1728 - 280].map((w) => mode(w))).toEqual(["preview", "preview", "preview", "preview", "preview"]);
    // A Mac 1280 window (1000) and a 1280 browser (960) cover, as a phone.
    expect(mode(1280 - 280)).toBeNull();
    expect(mode(1280 - 320)).toBeNull();
    expect(mode(PREVIEW_BESIDE_MIN_WIDTH - 1)).toBeNull();
    expect(mode(3000, false)).toBeNull();
    expect(mode(3000, true, false)).toBeNull();
  });

  test("on a laptop only a preview opens beside: a file a chat names, a Work file, one file of Brain's workspace", () => {
    const previews = [{ name: "files-preview" }, { name: "workFile" }, { name: "files-workspace", file: "worklog/x/index.html" }];
    expect(previews.every((route) => opensBeside(route, "preview"))).toBe(true);
    expect(previews.every((route) => opensBeside(route, "board"))).toBe(true);
    expect([session, work, settings, { name: "files-workspace" }, { name: "files-workspace", trail: ["worklog"] }].some((route) => opensBeside(route, "preview"))).toBe(false);
    expect(isPreview({ name: "files-workspace", file: "" })).toBe(false);
    expect(opensBeside(session, null)).toBe(false);
  });

  test("the pane shows on the board with Brain alone, on a laptop only while a preview is open", () => {
    expect(besideShows("board", 1, 0)).toBe(true);
    expect(besideShows("preview", 1, 0)).toBe(false);
    expect(besideShows("preview", 1, 1)).toBe(true);
    expect(besideShows("preview", 2, 1)).toBe(false);
    expect(besideShows(null, 1, 1)).toBe(false);
  });

  test("a second preview goes over the first (Back returns to it); the same one again changes nothing; a Session replaces", () => {
    const a = { name: "files-preview", reference: "/a.html" };
    const b = { name: "files-preview", reference: "/b.pdf" };
    expect(openedBeside([a], [b])).toEqual([a, b]);
    expect(openedBeside([a, b], [{ ...b }])).toEqual([a, b]);
    expect(openedBeside([], [a])).toEqual([a]);
    expect(openedBeside([a], [session])).toEqual([session]);
    expect(openedBeside([session], [a])).toEqual([a]);
  });

  test("crossing gates: a preview stays beside in either split; a Session leaving the board goes on the stack; out of both, the stack", () => {
    const file = { name: "files-preview", reference: "/a.html" };
    // Laptop to narrow: the preview covers the thread, as on a phone, still open.
    expect(crossBeside(null, [home], [file])).toEqual({ main: [home, file], beside: [] });
    // Narrow to laptop: a preview over Brain moves beside it; a Session over Brain stays over it.
    expect(crossBeside("preview", [home, file], [])).toEqual({ main: [home], beside: [file] });
    expect(crossBeside("preview", [home, session], [])).toEqual({ main: [home, session], beside: [] });
    expect(crossBeside("preview", [home, file, session], [])).toEqual({ main: [home, file, session], beside: [] });
    // Board to laptop: a preview stays; a Work and its file go on the stack together.
    expect(crossBeside("preview", [home], [file])).toEqual({ main: [home], beside: [file] });
    expect(crossBeside("preview", [home], [work, { name: "workFile", id: "f" }])).toEqual({ main: [home, work, { name: "workFile", id: "f" }], beside: [] });
    // Laptop to board: what stands beside stays; a Session over Brain moves beside.
    expect(crossBeside("board", [home], [file])).toEqual({ main: [home], beside: [file] });
    expect(crossBeside("board", [home, session], [])).toEqual({ main: [home], beside: [session] });
    // Settings over Brain: the stack stays; beside pages that may not stay close.
    expect(crossBeside("preview", [home, settings], [session])).toEqual({ main: [home, settings], beside: [] });
    expect(crossBeside(null, [home], [])).toEqual({ main: [home], beside: [] });
  });

  test("the preview split: 55 % to the preview, the thread never under 480, the preview never under 540", () => {
    expect(previewSplit(1232, 0.55)).toEqual({ thread: 551, preview: 673 });
    expect(previewSplit(1046, 0.55)).toEqual({ thread: 480, preview: 558 });
    expect(previewSplit(1448, 0.9)).toEqual({ thread: 480, preview: 960 });
    expect(previewSplit(1448, 0.1)).toEqual({ thread: 900, preview: 540 });
    // Narrower than both (a docked screen's strip): the thread keeps its 480.
    expect(previewSplit(1000, 0.55).thread).toBe(480);
    // A width dragged to, as the share kept: held to the limits.
    expect(previewSplit(1232, shareOf(1232, 700)).preview).toBe(700);
    expect(previewSplit(1232, shareOf(1232, 2000)).preview).toBe(1232 - 8 - 480);
    expect(previewSplit(1232, shareOf(1232, 100)).preview).toBe(540);
    expect(storedShare(null)).toBe(0.55);
    expect(storedShare("x")).toBe(0.55);
    expect(storedShare("0.62")).toBe(0.62);
  });

  test("a Session, a Work and a Work file open beside; Settings and Calendar cover both", () => {
    expect([session, work, { name: "workFile", id: "f" }, { name: "files-preview" }].every((route) => opensBeside(route))).toBe(true);
    expect([settings, { name: "calendar" }, { name: "computer" }, undefined].some((route) => opensBeside(route))).toBe(false);
  });

  test("crossing in moves the pages above Brain beside it; crossing out puts them back on the stack", () => {
    expect(intoBeside([home, work, session])).toEqual({ main: [home], beside: [work, session] });
    expect(intoBeside([home])).toBeNull();
    expect(intoBeside([home, settings, session])).toBeNull();
    expect(outOfBeside([home], [work, session])).toEqual([home, work, session]);
    expect(outOfBeside([home], [])).toBeNull();
    expect(outOfBeside([home, settings], [work])).toBeNull();
  });

  test("what the URL reads: Brain and what stands beside it while Brain shows", () => {
    expect(shownRoutes([home], [session], true)).toEqual([home, session]);
    expect(shownRoutes([home, settings], [session], true)).toEqual([home, settings]);
    expect(shownRoutes([home], [session], false)).toEqual([home]);
  });
});
