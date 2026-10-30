import { describe, expect, test } from "bun:test";
import {
  displayName,
  durationLabel,
  mediaLayout,
  singleMediaAspect,
  friendsErrorMessage,
  initials,
  notificationRows,
  relativeTime,
  type FriendsAuthor,
  type FriendsNotification,
} from "./friendsApi";

const author = (handle: string, agent = ""): FriendsAuthor => ({ id: `id-${handle}`, handle, agent_name: agent, pet_id: "cat" });

describe("friends presentation", () => {
  test("relative time reads like the mocks", () => {
    const now = Date.UTC(2026, 9, 26, 12, 0, 0);
    expect(relativeTime(now - 5_000, now)).toBe("now");
    expect(relativeTime(now - 12 * 60_000, now)).toBe("12m");
    expect(relativeTime(now - 3 * 3_600_000, now)).toBe("3h");
    expect(relativeTime(now - 30 * 3_600_000, now)).toBe("yesterday");
    expect(relativeTime(now - 5 * 86_400_000, now)).toBe("5d");
  });

  test("a person is shown by their agent's name, else their handle", () => {
    expect(displayName(author("lena", "Dango"))).toBe("Dango");
    expect(displayName(author("lena"))).toBe("@lena");
    expect(initials(author("lena"))).toBe("LE");
    expect(initials(author("lena", "dango"))).toBe("DA");
  });

  test("likes on one post fold into one row", () => {
    const note = (seq: number, kind: FriendsNotification["kind"], handle: string, post = "p1"): FriendsNotification => ({
      seq,
      kind,
      actor: author(handle),
      post_id: kind === "follow" ? "" : post,
      post_text: "Was it the shutdown hook?",
      created_at: seq,
      read: seq < 3,
    });
    const rows = notificationRows([note(5, "comment", "kasper"), note(4, "like", "lena"), note(3, "like", "june"), note(2, "like", "kasper"), note(1, "follow", "june")]);
    expect(rows.map((row) => row.text)).toEqual([
      "@kasper commented on your post",
      "@lena and 2 others liked your post",
      "@june followed you",
    ]);
    expect(rows[1].unread).toBe(true);
    expect(rows[2].detail).toBeUndefined();
  });

  test("service codes become sentences", () => {
    expect(friendsErrorMessage("account_too_new")).toContain("30 days");
    expect(friendsErrorMessage("weird_code")).toBe("Friends answered: weird_code");
    expect(friendsErrorMessage("refused", "It contains an email address")).toBe("It contains an email address");
    expect(friendsErrorMessage("refused")).toBe("Friends will not post that");
    expect(friendsErrorMessage(undefined)).toBe("Friends did not answer");
  });

  test("media errors become sentences", () => {
    expect(friendsErrorMessage("auto_media_off")).toContain("Auto-post");
    expect(friendsErrorMessage("video_too_long")).toContain("longer");
    expect(friendsErrorMessage("media_not_reviewed")).toContain("review");
  });

  test("a video shows its length as m:ss", () => {
    expect(durationLabel(14_133)).toBe("0:14");
    expect(durationLabel(60_000)).toBe("1:00");
    expect(durationLabel(0)).toBe("0:00");
  });

  test("pictures lay out as 1, 2, 3 or 4", () => {
    expect([1, 2, 3, 4, 5].map(mediaLayout)).toEqual(["single", "pair", "trio", "quad", "quad"]);
    expect(mediaLayout(0)).toBe("single");
  });

  test("a single picture is held between a wide and a tall frame", () => {
    expect(singleMediaAspect(720, 720)).toBe(1);
    expect(singleMediaAspect(4000, 500)).toBe(1.91);
    expect(singleMediaAspect(500, 4000)).toBe(0.8);
    expect(singleMediaAspect(0, 0)).toBe(1);
  });
});
