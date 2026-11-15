import type { SessionFileBinarySource } from "./sessionFilePreview";

/** A phone grant is only accepted from the upload owner, never provider text. */
export type ImageSource =
  | { kind: "phone"; uri: string; name: string; mimeType?: string }
  | { kind: "external"; uri: string; name: string; mimeType?: string }
  | { kind: "inline"; uri: string; name: string; mimeType?: string }
  | { kind: "owned"; path: string; name: string; mimeType?: string };

export function isImageAttachment(value: { mimeType?: string; name?: string; path?: string }) {
  return Boolean(value.mimeType?.startsWith("image/") || /\.(png|jpe?g|gif|webp|bmp|heic|heif|avif|svg)(?:[?#].*)?$/i.test((value.name || value.path || "").trim()));
}

export function isSvgImage(source: ImageSource, resolved?: SessionFileBinarySource) {
  if ([source.mimeType, resolved?.mimeType].some((type) => type?.toLowerCase().split(";", 1)[0] === "image/svg+xml")) return true;
  if (/\.svg(?:[?#].*)?$/i.test(source.name) || (source.kind === "owned" && /\.svg(?:[?#].*)?$/i.test(source.path))) return true;
  const uri = resolved?.uri || (source.kind === "owned" ? "" : source.uri);
  return /^data:image\/svg\+xml(?:;|,)/i.test(uri) || /\.svg(?:[?#].*)?$/i.test(uri);
}
