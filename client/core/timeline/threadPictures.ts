import type { ActivityTimelineItem } from "./activityTypes";
import { isImageAttachment } from "./imageSource";
import type { DisplayAttachment, MessageTimelineItem, TimelineItem } from "./types";

/** A picture of a thread, as the viewer steps through them (SPEC §1.7). */
export type ThreadPicture = {
  /** The row it is in and its source: what a tap on it names. */
  key: string;
  /** A web or data URL, or a path on the computer (read through its Session). */
  src: string;
  alt: string;
};

/** Which picture of a row a tap names. */
export function pictureKey(itemId: string, src: string): string {
  return `${itemId}|${src}`;
}

// `![alt](src)` and `![alt](<src> "title")`: what a reply shows as a picture.
const MARKDOWN_IMAGE = /!\[([^\]]*)\]\(\s*(?:<([^>]+)>|([^)\s]+))(?:\s+"[^"]*")?\s*\)/g;

/** The pictures a reply's Markdown shows, in order. */
export function markdownPictures(body: string): { src: string; alt: string }[] {
  return [...body.matchAll(MARKDOWN_IMAGE)].map((match) => ({ src: (match[2] ?? match[3] ?? "").trim(), alt: match[1] ?? "" })).filter((image) => image.src);
}

/** The pictures among a sent message's attachments (other files stay file rows). */
export function sentPictures(attachments: readonly DisplayAttachment[]): DisplayAttachment[] {
  return attachments.filter((file) => file.path && isImageAttachment(file));
}

/** The pictures a tool row (or each step of a folded run) produced: an agent's screenshot, an image tool's output. */
export function toolPictures(item: ActivityTimelineItem): string[] {
  const rows = item.steps?.length ? item.steps : [item];
  return [...new Set(rows.flatMap((row) => (row.previewPath ? [row.previewPath] : [])))];
}

/** Every picture of a thread, in the order it shows them: sent pictures, a reply's, a tool call's. Each row's source once. */
export function threadPictures(items: readonly TimelineItem[]): ThreadPicture[] {
  const out: ThreadPicture[] = [];
  const seen = new Set<string>();
  const add = (itemId: string, src: string, alt: string) => {
    const key = pictureKey(itemId, src);
    if (seen.has(key)) return;
    seen.add(key);
    out.push({ key, src, alt });
  };
  for (const item of items) {
    if (item.type === "message") {
      const message = item as MessageTimelineItem;
      if (message.role === "user") sentPictures(message.attachments).forEach((file) => add(message.id, file.path, file.name));
      else markdownPictures(message.body).forEach((image) => add(message.id, image.src, image.alt));
    } else if (item.type === "activity") {
      toolPictures(item as ActivityTimelineItem).forEach((src) => add(item.id, src, (item as ActivityTimelineItem).title));
    }
  }
  return out;
}

/** The same pictures in the same order: a streaming reply's next chunk leaves the viewer's slides as they are. */
export function samePictures(a: readonly ThreadPicture[], b: readonly ThreadPicture[]): boolean {
  return a.length === b.length && a.every((picture, at) => picture.key === b[at].key && picture.alt === b[at].alt);
}
