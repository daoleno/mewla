import { describe, expect, test } from "bun:test";
import {
  SESSION_FILE_MEDIA_RECOVERY_LIMIT,
  sessionFileMediaKindForPath,
  sessionFileMediaRecovery,
} from "./sessionFileMedia";
import { classifySessionFileRenderer, recognizeSessionFileReference } from "./sessionFilePreview";

describe("session file media", () => {
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
