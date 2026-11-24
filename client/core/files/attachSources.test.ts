import { describe, expect, test } from "bun:test";
import { attachOffer, pickedPhotoName } from "./attachSources";

describe("what the composer's + offers", () => {
  test("a phone gets the sheet: Photos first, Camera when the host can take one, Files", () => {
    expect(attachOffer({ os: "android", canCapture: true })).toEqual({ kind: "sheet", sources: ["photos", "camera", "files"] });
    expect(attachOffer({ os: "ios", canCapture: true })).toEqual({ kind: "sheet", sources: ["photos", "camera", "files"] });
  });

  test("no camera (an app without the new host call, a simulator, a tablet without one): two tiles", () => {
    expect(attachOffer({ os: "android", canCapture: false })).toEqual({ kind: "sheet", sources: ["photos", "files"] });
    expect(attachOffer({ os: "ios", canCapture: false })).toEqual({ kind: "sheet", sources: ["photos", "files"] });
  });

  test("desktops and the web open the file dialog at once", () => {
    for (const os of ["macos", "windows", "linux", "web"]) expect(attachOffer({ os, canCapture: false })).toEqual({ kind: "dialog" });
  });
});

describe("a picked photo's name", () => {
  const at = new Date(2026, 10, 24, 9, 15, 0);
  test("a media id or a bare id becomes the pick's time", () => {
    expect(pickedPhotoName("1000000021.png", "image/png", at)).toBe("Photo 2026-11-24 09.15.00.png");
    expect(pickedPhotoName("1000000022.jpg", "image/jpeg", at, 1)).toBe("Photo 2026-11-24 09.15.00 2.jpg");
    expect(pickedPhotoName("3F2504E0-4F89-11D3-9A0C-0305E82C3301", "image/jpeg", at)).toBe("Photo 2026-11-24 09.15.00.jpg");
  });
  test("a real name is kept", () => {
    expect(pickedPhotoName("Screenshot_20261124-091500.png", "image/png", at)).toBe("Screenshot_20261124-091500.png");
    expect(pickedPhotoName("IMG_0042.jpg", "image/jpeg", at)).toBe("IMG_0042.jpg");
  });
});
