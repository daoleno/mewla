import { beforeEach, describe, expect, test } from "bun:test";
import { installMemoryPlatform, memoryStore } from "../testing/memoryPlatform";
import {
  loadReadingPlaces,
  READING_PLACE_KEEP_MS,
  READING_PLACES_KEY,
  readingPlace,
  readingPlaceIndex,
  keepReadingPlaces,
  readingPlaceOf,
  rememberReadingPlace,
  resetReadingPlacesForTest,
} from "./readingPlace";

describe("a chat's reading place", () => {
  test("is the first row on screen, by its key, not a pixel offset", () => {
    const tokens = [
      { key: "m12", index: 12, isViewable: true },
      { key: "m10", index: 10, isViewable: true },
      { key: "m9", index: 9, isViewable: false },
      { key: "m11", index: 11, isViewable: true },
    ];
    expect(readingPlace(tokens)).toBe("m10");
    expect(readingPlace([])).toBeUndefined();
  });

  test("reopens at that row's index; a row that is gone keeps nothing", () => {
    const rows = [{ id: "d1" }, { id: "m9" }, { id: "m10" }, { id: "m11" }];
    expect(readingPlaceIndex(rows, "m10")).toBe(2);
    expect(readingPlaceIndex(rows, "gone")).toBeUndefined();
    expect(readingPlaceIndex(rows, undefined)).toBeUndefined();
  });
});

describe("reading places kept across a start of the app", () => {
  let clock = 1_000_000;
  const now = () => clock;
  const MIN = 60_000;
  const saved = () => new Promise((resolve) => setTimeout(resolve, 600));
  /** The app starts again (a reload): nothing in memory, the storage as it was. */
  const startAgain = async () => {
    resetReadingPlacesForTest();
    await loadReadingPlaces(now);
  };
  beforeEach(async () => {
    clock = 1_000_000;
    resetReadingPlacesForTest();
    installMemoryPlatform({ storage: memoryStore() });
    await loadReadingPlaces(now);
  });

  test("a place read to is kept, and the app started again opens there", async () => {
    rememberReadingPlace("brain:s1:chat:t1", "m12", now);
    rememberReadingPlace("session:w1", "e40", now);
    await saved();
    await startAgain();
    expect(readingPlaceOf("brain:s1:chat:t1")).toBe("m12");
    expect(readingPlaceOf("session:w1")).toBe("e40");
  });

  test("back at the end, the chat opens at its end again", async () => {
    rememberReadingPlace("brain:s1:chat:t1", "m12", now);
    rememberReadingPlace("brain:s1:chat:t1", undefined, now);
    await saved();
    await startAgain();
    expect(readingPlaceOf("brain:s1:chat:t1")).toBeUndefined();
  });

  test("while the app runs a place never expires, however long one row stays on top", () => {
    rememberReadingPlace("brain:s1:chat:t1", "m12", now);
    clock += 60 * MIN;
    expect(readingPlaceOf("brain:s1:chat:t1")).toBe("m12");
  });

  test("the keep window counts from when the app last kept the place, not from the scroll", async () => {
    rememberReadingPlace("brain:s1:chat:t1", "m12", now);
    await saved();
    // Read without scrolling for 14 minutes, then the app leaves the front; it is made again 6 minutes later.
    clock += 14 * MIN;
    keepReadingPlaces(now);
    clock += 6 * MIN;
    await startAgain();
    expect(readingPlaceOf("brain:s1:chat:t1")).toBe("m12");
    // Started again longer after than that, the chat opens at its end.
    clock += READING_PLACE_KEEP_MS;
    await startAgain();
    expect(readingPlaceOf("brain:s1:chat:t1")).toBeUndefined();
  });

  test("nothing is kept before the places from before this start are read", async () => {
    installMemoryPlatform({ storage: memoryStore({ [READING_PLACES_KEY]: JSON.stringify({ a: { key: "m2", at: clock } }) }) });
    resetReadingPlacesForTest();
    // The app leaves the front before its places are read: that must not write over them.
    keepReadingPlaces(now);
    await loadReadingPlaces(now);
    expect(readingPlaceOf("a")).toBe("m2");
  });

  test("unreadable storage opens chats at their end", async () => {
    installMemoryPlatform({ storage: memoryStore({ [READING_PLACES_KEY]: "{not json" }) });
    await startAgain();
    expect(readingPlaceOf("brain:s1:chat:t1")).toBeUndefined();
  });
});
