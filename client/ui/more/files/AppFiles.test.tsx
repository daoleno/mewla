import { describe, expect, test } from "nsh/test";
import { appFileImageUrl, appFileUploadHost, keepAppFiles, releaseAppFiles } from "./appFiles";

describe("the app's own files (pasted and dropped)", () => {
  test("a file whose chip went is let go: nothing holds its bytes, and it cannot be sent again", async () => {
    const [kept] = keepAppFiles([new File([new Uint8Array(1024)], "clip.mov", { type: "video/quicktime" })]);
    const [picture] = keepAppFiles([new File([new Uint8Array(8)], "shot.png", { type: "image/png" })]);
    expect(appFileImageUrl(picture!.uri)).toBe(appFileImageUrl(picture!.uri));
    releaseAppFiles([kept!.uri, picture!.uri]);
    expect(appFileImageUrl(picture!.uri)).toBeNull();
    const request = { uploadId: "u1", fileUri: kept!.uri, url: "http://desk.test:9877/upload", headers: {} };
    await expect(appFileUploadHost.upload(request as never)).rejects.toThrow("Choose the file again to upload it.");
  });
});
