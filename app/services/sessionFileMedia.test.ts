import { describe, expect, test } from "bun:test";
import {
  formatSessionFileMediaTime,
  parseSessionFileMediaEvent,
  SESSION_FILE_MEDIA_RECOVERY_LIMIT,
  sessionFileMediaKindForPath,
  sessionFileMediaProgress,
  sessionFileMediaRecovery,
  sessionFileMediaScrubTime,
} from "./sessionFileMedia";
import { classifySessionFileRenderer, recognizeSessionFileReference } from "./sessionFilePreview";

describe("session file media", () => {
  test("formats time like a player, with hours only for long media", () => {
    expect(formatSessionFileMediaTime(0)).toBe("0:00");
    expect(formatSessionFileMediaTime(30.9)).toBe("0:30");
    expect(formatSessionFileMediaTime(29.98)).toBe("0:30");
    expect(formatSessionFileMediaTime(29.9)).toBe("0:29");
    expect(formatSessionFileMediaTime(64)).toBe("1:04");
    expect(formatSessionFileMediaTime(750)).toBe("12:30");
    expect(formatSessionFileMediaTime(3723)).toBe("1:02:03");
    expect(formatSessionFileMediaTime(30, 3723)).toBe("0:00:30");
    expect(formatSessionFileMediaTime(Number.NaN)).toBe("0:00");
    expect(formatSessionFileMediaTime(-4)).toBe("0:00");
  });

  test("maps a point on the scrubber to a clamped time", () => {
    expect(sessionFileMediaScrubTime(100, 400, 64)).toBe(16);
    expect(sessionFileMediaScrubTime(-20, 400, 64)).toBe(0);
    expect(sessionFileMediaScrubTime(500, 400, 64)).toBe(64);
    expect(sessionFileMediaScrubTime(100, 0, 64)).toBe(0);
    expect(sessionFileMediaScrubTime(100, 400, 0)).toBe(0);
    expect(sessionFileMediaProgress(16, 64)).toBe(0.25);
    expect(sessionFileMediaProgress(80, 64)).toBe(1);
    expect(sessionFileMediaProgress(5, 0)).toBe(0);
  });

  test("re-signs a lapsed stream only after it played, and only a few times", () => {
    expect(sessionFileMediaRecovery({ loadedSinceSource: false, attempts: 0 })).toBe("fail");
    expect(sessionFileMediaRecovery({ loadedSinceSource: true, attempts: 0 })).toBe("reauthorize");
    expect(
      sessionFileMediaRecovery({
        loadedSinceSource: true,
        attempts: SESSION_FILE_MEDIA_RECOVERY_LIMIT,
      }),
    ).toBe("fail");
  });

  test("accepts only well-formed player messages", () => {
    expect(parseSessionFileMediaEvent("not json")).toBeNull();
    expect(parseSessionFileMediaEvent(JSON.stringify({ type: "size" }))).toBeNull();
    expect(parseSessionFileMediaEvent(JSON.stringify({ type: "error", code: 2 }))).toEqual({
      type: "error",
      code: 2,
    });
    expect(
      parseSessionFileMediaEvent(
        JSON.stringify({
          type: "status",
          currentTime: 30.2,
          duration: "64",
          paused: false,
          muted: false,
          loaded: true,
        }),
      ),
    ).toEqual({
      type: "status",
      status: {
        currentTime: 30.2,
        duration: 0,
        paused: false,
        muted: false,
        ended: false,
        waiting: false,
        loaded: true,
      },
    });
  });

  test("chooses a player from the extension where the daemon is asked later", () => {
    expect(sessionFileMediaKindForPath("promo/v2/web.mp4")).toBe("video");
    expect(sessionFileMediaKindForPath("take.MOV")).toBe("video");
    expect(sessionFileMediaKindForPath("voice.m4a")).toBe("audio");
    expect(sessionFileMediaKindForPath("song.mp3")).toBe("audio");
    expect(sessionFileMediaKindForPath("notes.md")).toBeNull();
  });

  test("bare media names in a reply open as files and render as media", () => {
    expect(recognizeSessionFileReference("web.mp4")).toBe("web.mp4");
    expect(recognizeSessionFileReference("memo.wav")).toBe("memo.wav");
    expect(
      recognizeSessionFileReference("/home/me/acelabs/freeride-promo/v2-manifesto-type/web.mp4"),
    ).toBe("/home/me/acelabs/freeride-promo/v2-manifesto-type/web.mp4");
    expect(classifySessionFileRenderer("video")).toBe("video");
    expect(classifySessionFileRenderer("audio")).toBe("audio");
  });
});
