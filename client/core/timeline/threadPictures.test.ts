import { describe, expect, test } from "bun:test";
import type { ActivityTimelineItem } from "./activityTypes";
import { markdownPictures, pictureKey, samePictures, sentPictures, threadPictures, toolPictures } from "./threadPictures";
import type { MessageTimelineItem, TimelineItem } from "./types";

const message = (id: string, role: "user" | "assistant", body: string, attachments: MessageTimelineItem["attachments"] = []): MessageTimelineItem => ({ type: "message", id, role, body, attachments });
const tool = (id: string, patch: Partial<ActivityTimelineItem>): ActivityTimelineItem => ({ type: "activity", id, title: "Took a screenshot", tone: "success", icon: "image", ...patch });

describe("the pictures of a thread", () => {
  test("a reply's Markdown pictures, in order, with their alt text", () => {
    expect(markdownPictures("Before ![the login page](shots/login.png) and ![](<shots/two words.png> \"Two\") after")).toEqual([
      { src: "shots/login.png", alt: "the login page" },
      { src: "shots/two words.png", alt: "" },
    ]);
    expect(markdownPictures("[a link](https://example.com) is no picture")).toEqual([]);
  });

  test("sent pictures are the attachments that are images; other files stay files", () => {
    const files = [
      { name: "shot.png", path: "/state/uploads/shot.png" },
      { name: "report.pdf", path: "/state/uploads/report.pdf", mimeType: "application/pdf" },
      { name: "clip", path: "/state/uploads/clip", mimeType: "image/jpeg" },
    ];
    expect(sentPictures(files).map((file) => file.name)).toEqual(["shot.png", "clip"]);
  });

  test("a tool row's picture, and each step's in a folded run, once", () => {
    expect(toolPictures(tool("t", { previewPath: "/tmp/a.png" }))).toEqual(["/tmp/a.png"]);
    expect(toolPictures(tool("run", { steps: [tool("1", { previewPath: "/tmp/a.png" }), tool("2", {}), tool("3", { previewPath: "/tmp/b.png" }), tool("4", { previewPath: "/tmp/a.png" })] }))).toEqual(["/tmp/a.png", "/tmp/b.png"]);
  });

  test("the viewer steps through every picture of the thread in the order the thread shows them", () => {
    const items: TimelineItem[] = [
      message("u1", "user", "What is on this?", [{ name: "shot.png", path: "/state/uploads/shot.png" }]),
      tool("t1", { previewPath: "/tmp/screen.png" }),
      message("a1", "assistant", "Here: ![the page](page.png)\n\nAnd again ![the page](page.png)"),
    ];
    expect(threadPictures(items)).toEqual([
      { key: pictureKey("u1", "/state/uploads/shot.png"), src: "/state/uploads/shot.png", alt: "shot.png" },
      { key: pictureKey("t1", "/tmp/screen.png"), src: "/tmp/screen.png", alt: "Took a screenshot" },
      { key: pictureKey("a1", "page.png"), src: "page.png", alt: "the page" },
    ]);
  });

  test("a reply that grows without a new picture leaves the pictures as they were", () => {
    const before = threadPictures([message("a1", "assistant", "![a](a.png) Look")]);
    expect(samePictures(before, threadPictures([message("a1", "assistant", "![a](a.png) Look here, the button")]))).toBe(true);
    expect(samePictures(before, threadPictures([message("a1", "assistant", "![a](a.png) Look ![b](b.png)")]))).toBe(false);
  });
});
