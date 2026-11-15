import { expect, test } from "bun:test";
import { isImageAttachment, isSvgImage } from "./imageSource";

test("SVG metadata is read from the source and what it resolved to", () => {
  expect(isSvgImage({ kind: "inline", uri: "data:image/svg+xml;base64,YQ==", name: "Image" })).toBe(true);
  expect(isImageAttachment({ name: "logo.svg" })).toBe(true);
  expect(isSvgImage({ kind: "phone", uri: "content://picker/grant", name: "logo", mimeType: "image/svg+xml" })).toBe(true);
  expect(isSvgImage({ kind: "owned", path: "/workspace/logo.svg", name: "Image" })).toBe(true);
  expect(isSvgImage({ kind: "owned", path: "opaque-reference", name: "Logo" }, { uri: "https://owned.example.test/stream", headers: {}, mimeType: "image/svg+xml" })).toBe(true);
});
