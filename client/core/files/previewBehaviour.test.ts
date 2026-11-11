import { describe, expect, test } from "bun:test";
import { dirnameOf, resolveAgainst, resolveMarkdownLink } from "./markdownLinks";
import { createSessionFileMediaRecovery, SESSION_FILE_MEDIA_RECOVERY_LIMIT } from "./sessionFileMedia";

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

function player(kind: "video" | "audio" = "video", resolve: () => Promise<{ uri: string }> = async () => ({ uri: "fresh" })) {
  const loads: Array<{ uri: string; at: number; play: boolean }> = [];
  const failures: Array<{ message: string; stale: boolean }> = [];
  const recovery = createSessionFileMediaRecovery({
    kind,
    resolve,
    load: (source, at, play) => loads.push({ uri: source.uri, at, play }),
    fail: (message, stale) => failures.push({ message, stale }),
    isStale: (error) => error instanceof Error && error.message.includes("changed"),
  });
  return { recovery, loads, failures };
}

describe("media playback past the 2-minute capability", () => {
  test("an error after data played signs a fresh stream and resumes there, playing as it was", async () => {
    const p = player();
    p.recovery.loaded();
    p.recovery.progress(10);
    p.recovery.progress(42.5);
    p.recovery.error();
    await tick();
    expect(p.loads).toEqual([{ uri: "fresh", at: 42.5, play: true }]);
    expect(p.failures).toEqual([]);
  });

  test("an error before any data says the file can't be played here, by its kind", () => {
    const video = player("video");
    video.recovery.error();
    expect(video.failures).toEqual([{ message: "This video can't be played here. Download it to play it in another app.", stale: false }]);
    const audio = player("audio");
    audio.recovery.error();
    expect(audio.failures[0]!.message).toBe("This audio can't be played here. Download it to play it in another app.");
  });

  test("it gives up after the limit of re-signs in a row, and moving playback resets the count", async () => {
    const p = player();
    for (let i = 0; i < SESSION_FILE_MEDIA_RECOVERY_LIMIT; i++) {
      p.recovery.loaded();
      p.recovery.error();
      await tick();
    }
    expect(p.loads).toHaveLength(SESSION_FILE_MEDIA_RECOVERY_LIMIT);
    p.recovery.loaded();
    p.recovery.error();
    expect(p.failures).toEqual([{ message: "Playback stopped and couldn't resume. Refresh and try again.", stale: false }]);

    const healthy = player();
    healthy.recovery.loaded();
    healthy.recovery.error();
    await tick();
    healthy.recovery.loaded();
    healthy.recovery.progress(5);
    for (let i = 0; i < SESSION_FILE_MEDIA_RECOVERY_LIMIT; i++) {
      healthy.recovery.loaded();
      healthy.recovery.error();
      await tick();
    }
    expect(healthy.failures).toEqual([]);
  });

  test("a refused re-sign says why, stale when the file changed; a paused player resumes paused", async () => {
    const p = player("video", async () => {
      throw new Error("The Session or file changed before the preview could be authorized. Refresh and try again.");
    });
    p.recovery.loaded();
    p.recovery.error();
    await tick();
    expect(p.failures).toEqual([{ message: "The Session or file changed before the preview could be authorized. Refresh and try again.", stale: true }]);

    const quiet = player("video", async () => {
      throw "nope";
    });
    quiet.recovery.loaded();
    quiet.recovery.error();
    await tick();
    expect(quiet.failures[0]!.message).toBe("Could not authorize the stream again.");

    const paused = player();
    paused.recovery.loaded();
    paused.recovery.progress(3);
    paused.recovery.paused();
    paused.recovery.error();
    await tick();
    expect(paused.loads[0]!.play).toBe(false);
  });
});

describe("links inside a previewed Markdown file", () => {
  test("a relative link is a file beside the previewed one; web links open outside; other schemes never run", () => {
    const base = dirnameOf("/work/docs/guide.md");
    expect(base).toBe("/work/docs");
    expect(resolveMarkdownLink("./setup.md", base)).toEqual({ kind: "file", reference: "/work/docs/setup.md" });
    expect(resolveMarkdownLink("../src/app.ts:12", base)).toEqual({ kind: "file", reference: "/work/src/app.ts" });
    expect(resolveMarkdownLink("/etc/hosts", base)).toEqual({ kind: "file", reference: "/etc/hosts" });
    expect(resolveMarkdownLink("my%20notes.md#top", base)).toEqual({ kind: "file", reference: "/work/docs/my notes.md" });
    expect(resolveMarkdownLink("https://example.com/a", base)).toEqual({ kind: "external", url: "https://example.com/a" });
    expect(resolveMarkdownLink("javascript:alert(1)", base)).toEqual({ kind: "blocked" });
    expect(resolveMarkdownLink("#section", base)).toEqual({ kind: "blocked" });
    expect(resolveAgainst(undefined, "a.md")).toBe("a.md");
    expect(dirnameOf("a.md")).toBe("");
  });
});
