import { describe, expect, test } from "bun:test";
import { dataUrlMediaType, isDirectPicture, pictureExtension, pictureFailure, pictureFileName, pictureStamp } from "./pictureSave";

const NOW = new Date(2026, 10, 19, 23, 45, 12);

describe("pictureFileName", () => {
  test("a computer path keeps its name", () => {
    expect(pictureFileName("/tmp/screen.png", "image/png", NOW)).toBe("screen.png");
    expect(pictureFileName("file:///home/me/shots/login.png", "image/png", NOW)).toBe("login.png");
  });

  test("the extension is what the bytes are, not what the path says", () => {
    expect(pictureFileName("/home/me/shots/login.jpg", "image/gif", NOW)).toBe("login.gif");
    expect(pictureFileName("/tmp/photo.JPEG", "image/jpeg", NOW)).toBe("photo.jpg");
    expect(pictureFileName("/tmp/no-extension", "image/webp", NOW)).toBe("no-extension.webp");
  });

  test("an unknown type keeps the path's extension", () => {
    expect(pictureFileName("/tmp/odd.XYZ", "", NOW)).toBe("odd.xyz");
  });

  test("a web address names it by its last part, decoded, without its query", () => {
    expect(pictureFileName("https://example.com/a/b/cat%20pic.png?size=2#top", "image/png", NOW)).toBe("cat pic.png");
  });

  test("a data URL, or a name with nothing left, is named by the time it was saved", () => {
    expect(pictureFileName("data:image/png;base64,AAAA", "image/png", NOW)).toBe("mewla-20261119-234512.png");
    expect(pictureFileName("https://example.com/", "image/jpeg", NOW)).toBe("mewla-20261119-234512.jpg");
    expect(pictureFileName("/tmp/.png", "image/png", NOW)).toBe("mewla-20261119-234512.png");
  });

  test("characters a file system refuses are replaced", () => {
    expect(pictureFileName('C:\\shots\\a:b*c?.png', "image/png", NOW)).toBe("a_b_c_.png");
  });

  test("a very long name is cut, its extension kept", () => {
    const name = pictureFileName(`/tmp/${"x".repeat(300)}.png`, "image/png", NOW);
    expect(name.length).toBe(124);
    expect(name.endsWith(".png")).toBe(true);
  });
});

describe("media types", () => {
  test("a data URL's type, lower case and without parameters", () => {
    expect(dataUrlMediaType("data:image/PNG;base64,AAAA")).toBe("image/png");
    expect(dataUrlMediaType("data:image/svg+xml,<svg/>")).toBe("image/svg+xml");
    expect(dataUrlMediaType("https://example.com/a.png")).toBe("");
  });

  test("extensions of the types a chat shows", () => {
    expect(pictureExtension("image/jpeg; q=1")).toBe(".jpg");
    expect(pictureExtension("image/webp")).toBe(".webp");
    expect(pictureExtension("text/plain")).toBe("");
  });

  test("a direct picture shows as it is; anything else is a computer path", () => {
    expect(isDirectPicture("https://example.com/a.png")).toBe(true);
    expect(isDirectPicture("data:image/png;base64,AAAA")).toBe(true);
    expect(isDirectPicture("/tmp/screen.png")).toBe(false);
  });

  test("the time stamp", () => {
    expect(pictureStamp(new Date(2026, 0, 2, 3, 4, 5))).toBe("mewla-20260102-030405");
  });
});

describe("pictureFailure", () => {
  test("says what failed, in the host's words", () => {
    expect(pictureFailure("save", new Error("The gallery did not take the picture."))).toBe("Couldn't save the picture: The gallery did not take the picture");
    expect(pictureFailure("share", "")).toBe("Couldn't share the picture.");
  });

  test("a refused permission and a lost computer read as such", () => {
    expect(pictureFailure("save", new Error("Photos permission denied."))).toBe("Couldn't save the picture: Mewla isn't allowed to save pictures");
    expect(pictureFailure("copy", new Error("Daemon is not connected."))).toBe("Couldn't copy the picture: the computer isn't connected");
  });
});
