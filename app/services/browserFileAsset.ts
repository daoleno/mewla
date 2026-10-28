import type { UploadDocumentAsset } from "./uploads";

const PASTED_NAME_EXTENSION: Record<string, string> = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/gif": "gif",
  "image/webp": "webp",
  "image/svg+xml": "svg",
  "image/heic": "heic",
  "application/pdf": "pdf",
  "text/plain": "txt",
};

/**
 * A browser File (pasted, dropped) as an upload asset. A clipboard image has
 * the generic name "image.png", so it is named by when it was pasted.
 */
export function browserFileAsset(file: File, now: Date = new Date()): UploadDocumentAsset {
  const generic = !file.name || /^image\.[a-z0-9]+$/i.test(file.name);
  const extension = PASTED_NAME_EXTENSION[file.type] ?? "";
  const stamp = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-")
    + ` ${[now.getHours(), now.getMinutes(), now.getSeconds()].map((n) => String(n).padStart(2, "0")).join(".")}`;
  const name = generic ? `Pasted ${file.type.startsWith("image/") ? "image" : "file"} ${stamp}${extension ? `.${extension}` : ""}` : file.name;
  return {
    uri: typeof URL !== "undefined" && typeof URL.createObjectURL === "function" ? URL.createObjectURL(file) : "",
    name,
    mimeType: file.type || undefined,
    size: file.size,
    file,
  };
}

