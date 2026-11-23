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

const two = (n: number) => String(n).padStart(2, "0");

/**
 * The name a pasted or dropped file is uploaded under. A clipboard picture comes as
 * the generic "image.png" (or with no name), so it is named by when it was pasted:
 * `Pasted image 2026-11-11 08.05.09.png`; any other name is kept (v0.3.1 web).
 */
export function pastedFileName(name: string, type: string, now: Date = new Date()): string {
  const generic = !name || /^image\.[a-z0-9]+$/i.test(name);
  if (!generic) return name;
  const extension = PASTED_NAME_EXTENSION[type] ?? "";
  const stamp = `${now.getFullYear()}-${two(now.getMonth() + 1)}-${two(now.getDate())} ${two(now.getHours())}.${two(now.getMinutes())}.${two(now.getSeconds())}`;
  return `Pasted ${type.startsWith("image/") ? "image" : "file"} ${stamp}${extension ? `.${extension}` : ""}`;
}

/** What a paste into a composer holds, as NSH's `onPaste` reports it. */
export type PastedContent = { text: string; html: string; files: number; uris: readonly string[] };

/**
 * What of a paste becomes chips; its text always goes into the field as typed (the field inserts it itself; a Finder
 * copy's names it leaves out on the Mac):
 * - a file copied in a file manager (`uris`, the desktops): the file, whose contents come in `files` on the Mac
 *   (`localFileContents` sorts them from icons);
 * - pictures (`files`: a screenshot to the clipboard, Copy Image): the pictures, unless the paste is formatted
 *   text with a picture of itself, as a spreadsheet cell or a selection on a web page copies: then only its
 *   text. Text that is only a link is the picture's address (a browser's Copy Image), not text of its own.
 */
export function pasteAttaches(paste: PastedContent): { files: boolean; uris: boolean } {
  const text = paste.text.trim();
  const rich = text !== "" && paste.html.trim() !== "" && !/^https?:\/\/\S+$/i.test(text);
  return { files: paste.files > 0 && paste.uris.length === 0 && !rich, uris: paste.uris.length > 0 };
}

/** The local path a `file:` URL (a dropped or copied file, NSH's `uris`) names; null for anything else. */
export function localFilePath(uri: string): string | null {
  const match = /^file:\/\/(?:localhost)?(\/[^?#]*)$/i.exec(uri);
  if (!match) return null;
  try {
    return decodeURIComponent(match[1]!);
  } catch {
    return null;
  }
}

/** The name of the local file or folder `uri` names (its path's last part); null when it names none. */
export function localFileName(uri: string): string | null {
  return localFilePath(uri)?.split("/").filter(Boolean).at(-1) ?? null;
}

/**
 * Sorts what a drop or a paste delivered by the local files it named (`uris`: Finder's files, the desktops):
 * - `keep`, for each delivered file by its name (`names`): with no local files named, every one (a screenshot, an
 *   image from a page, a promised file); else only the named files' contents (an icon a file manager puts beside
 *   them is not one);
 * - `unread`: the named files that came without contents, which could not be read (the system refused, gone);
 * - `folders`: how many folders were named, which never come (a folder's URL ends in "/").
 */
export function localFileContents(names: readonly string[], uris: readonly string[]): { keep: boolean[]; unread: { uri: string; name: string }[]; folders: number } {
  const local = uris.flatMap((uri) => {
    const name = localFileName(uri);
    return name ? [{ uri, name }] : [];
  });
  const folders = local.filter(({ uri }) => uri.endsWith("/")).length;
  const files = local.filter(({ uri }) => !uri.endsWith("/"));
  const named = new Set(files.map(({ name }) => name));
  const given = new Set(names);
  return {
    keep: names.map((name) => !local.length || named.has(name)),
    unread: files.filter(({ name }) => !given.has(name)),
    folders,
  };
}

const TYPE_OF_EXTENSION: Record<string, string> = {
  ...Object.fromEntries(Object.entries(PASTED_NAME_EXTENSION).map(([type, extension]) => [extension, type])),
  jpeg: "image/jpeg",
  tif: "image/tiff",
  tiff: "image/tiff",
  md: "text/markdown",
  json: "application/json",
  mov: "video/quicktime",
  mp4: "video/mp4",
};

/** A local file's type by its name's extension, as a browser types a dropped file; "" when the name says none. */
export function fileTypeOf(name: string): string {
  const extension = /\.([a-z0-9]+)$/i.exec(name)?.[1]?.toLowerCase() ?? "";
  return TYPE_OF_EXTENSION[extension] ?? "";
}

/**
 * `after` less the `inserted` text an input put in at one place of `before` (the name Finder copies beside a file,
 * which the field inserts itself as the file becomes a chip): what to put back, and where the caret goes; null when
 * `after` is not `before` with exactly that put in (the person typed meanwhile, or the paste replaced a selection).
 */
export function withoutInserted(before: string, after: string, inserted: string): { value: string; caret: number } | null {
  if (!inserted || after.length !== before.length + inserted.length) return null;
  let at = 0;
  while (at < before.length && before[at] === after[at]) at += 1;
  // Anywhere up to the first difference (the name may start with what followed the caret).
  for (let start = at; start >= 0; start -= 1) {
    if (after.slice(start, start + inserted.length) === inserted && after.slice(start + inserted.length) === before.slice(start)) return { value: before, caret: start };
  }
  return null;
}
