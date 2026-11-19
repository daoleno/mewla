/**
 * Saving and sharing a picture a chat shows: what the saved file is called and what a
 * failure says. The bytes are the original's (the computer's file, read whole), never a
 * capture of the view; client/ui/chat/pictureActions.ts moves them.
 */

/** A web or data URL, which shows as it is; anything else is a path on the computer. */
export const isDirectPicture = (src: string) => /^(https?:|data:image\/)/i.test(src);

const EXTENSIONS: Record<string, string> = {
  "image/png": ".png",
  "image/jpeg": ".jpg",
  "image/jpg": ".jpg",
  "image/gif": ".gif",
  "image/webp": ".webp",
  "image/bmp": ".bmp",
  "image/svg+xml": ".svg",
  "image/heic": ".heic",
  "image/heif": ".heif",
  "image/avif": ".avif",
  "image/tiff": ".tiff",
  "image/x-icon": ".ico",
  "image/vnd.microsoft.icon": ".ico",
};

/** A media type without its parameters, lower case; "" when it is none. */
export function baseMediaType(type: string | null | undefined): string {
  return (type ?? "").split(";", 1)[0]!.trim().toLowerCase();
}

/** The media type a `data:` URL says it holds, "" for any other URL. */
export function dataUrlMediaType(url: string): string {
  const match = /^data:([^;,]*)[;,]/i.exec(url);
  return match ? baseMediaType(match[1]) : "";
}

/** The file extension of an image's media type (with its dot), "" when it is no image type it knows. */
export function pictureExtension(type: string): string {
  return EXTENSIONS[baseMediaType(type)] ?? "";
}

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

/** `mewla-20261119-234512`: the name of a picture that had none, from the time it was saved. */
export function pictureStamp(now: Date): string {
  return `mewla-${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
}

/** The last segment of a path or URL, decoded, without a query or fragment; "" for a `data:` URL. */
function lastSegment(src: string): string {
  const source = src.trim();
  if (/^data:/i.test(source)) return "";
  let path = source.replace(/^file:\/\//i, "");
  if (/^https?:/i.test(path)) {
    try {
      path = new URL(path).pathname;
    } catch {
      path = path.replace(/[?#].*$/, "");
    }
  }
  const segment = path.replace(/\\/g, "/").split("/").filter(Boolean).at(-1) ?? "";
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

/**
 * What a saved or shared picture is called: its file's name (the path's or URL's last part,
 * else `mewla-<time>`), with the extension of what the bytes really are (`type`, the media
 * type they came with), so a PNG called `shot.jpg` saves as `shot.png`.
 */
export function pictureFileName(src: string, type: string, now: Date = new Date()): string {
  const extension = pictureExtension(type);
  const raw = lastSegment(src)
    .replace(/[\u0000-\u001f\u007f]/g, "")
    .replace(/[/\\:*?"<>|]/g, "_")
    .trim();
  const dot = raw.lastIndexOf(".");
  const stem = (dot >= 0 ? raw.slice(0, dot) : raw).trim().replace(/^\.+/, "");
  const own = dot >= 0 ? raw.slice(dot).toLowerCase() : "";
  return `${(stem || pictureStamp(now)).slice(0, 120)}${extension || own}`;
}

export type PictureAction = "save" | "share" | "copy";

/** What failed, in the person's words: `Couldn't save the picture: …`. */
export function pictureFailure(action: PictureAction, error: unknown): string {
  const verb = action === "save" ? "save" : action === "share" ? "share" : "copy";
  const raw = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  const reason = pictureReason(raw);
  return reason ? `Couldn't ${verb} the picture: ${reason}` : `Couldn't ${verb} the picture.`;
}

function pictureReason(message: string): string {
  const text = message.trim().replace(/\.$/, "");
  if (!text) return "";
  if (/ERR_ACCESS_DENIED|permission|denied/i.test(text)) return "Mewla isn't allowed to save pictures";
  if (/not connected|no computer/i.test(text)) return "the computer isn't connected";
  if (/no picture|not a supported image/i.test(text)) return "the computer has no such picture";
  return text;
}
