/**
 * What the composer's "+" offers (docs/brain-and-work.md, "Attachments"): on a phone or a tablet a sheet of the
 * places a file comes from, the most common first; on a desktop or in a browser the system's file dialog at once
 * (photos are files there, and there is no camera to take one with).
 *
 * - `photos`: the system photo picker (newest first: the screenshot just taken is its first tile), no permission;
 * - `camera`: a photo taken now, when the app's host can take one (`files.canCapture`) on a device with a camera;
 * - `files`: the system document picker (Files, Downloads, another app's documents).
 *
 * A link or text copied a moment ago is the field's own paste, not an entry here.
 */
export type AttachSource = "photos" | "camera" | "files";

export type AttachOffer = { kind: "dialog" } | { kind: "sheet"; sources: AttachSource[] };

export function attachOffer(input: { os: string; canCapture: boolean }): AttachOffer {
  // A tablet is a phone here, a keyboard and trackpad on it or not: it has a camera and a photo library.
  if (input.os !== "ios" && input.os !== "android") return { kind: "dialog" };
  return { kind: "sheet", sources: input.canCapture ? ["photos", "camera", "files"] : ["photos", "files"] };
}

/** A tile's glyph and its one word. */
export const ATTACH_SOURCE_LOOK: Record<AttachSource, { icon: "image" | "camera" | "folder"; label: string }> = {
  photos: { icon: "image", label: "Photos" },
  camera: { icon: "camera", label: "Camera" },
  files: { icon: "folder", label: "Files" },
};

const two = (n: number) => String(n).padStart(2, "0");

/**
 * The name a photo from the photo picker goes up under. Android's picker names it by its media id
 * (`1000000021.png`) and a picker may give a bare id (a UUID): such a name says nothing to the person or the agent,
 * so it becomes `Photo 2026-11-24 09.15.00.png` (the pick's time; ` 2`, ` 3` … for the others of one pick). A real
 * name (`Screenshot_20261124-091500.png`, `IMG_0042.HEIC` made JPEG) is kept.
 */
export function pickedPhotoName(name: string, type: string, now: Date, index = 0): string {
  const dot = name.lastIndexOf(".");
  const stem = dot > 0 ? name.slice(0, dot) : name;
  const opaque = !stem || /^\d+$/.test(stem) || /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(stem);
  if (!opaque) return name;
  const extension = dot > 0 ? name.slice(dot) : type === "image/png" ? ".png" : type === "image/gif" ? ".gif" : type === "image/webp" ? ".webp" : ".jpg";
  const stamp = `${now.getFullYear()}-${two(now.getMonth() + 1)}-${two(now.getDate())} ${two(now.getHours())}.${two(now.getMinutes())}.${two(now.getSeconds())}`;
  return `Photo ${stamp}${index > 0 ? ` ${index + 1}` : ""}${extension}`;
}
