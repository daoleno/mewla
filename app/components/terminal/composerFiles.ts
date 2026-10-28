/**
 * Files that arrive through a browser clipboard or drag. A copied image has
 * no name of its own, and rich cells (a spreadsheet range) arrive as a
 * picture plus their text: when there is text, the text wins.
 */
export function clipboardFiles(data: Pick<DataTransfer, "files" | "getData"> | null | undefined): File[] {
  const files = Array.from(data?.files ?? []);
  if (!files.length) return [];
  const text = data?.getData("text/plain") ?? "";
  if (text.trim() && files.every((file) => file.type.startsWith("image/"))) return [];
  return files;
}

/** A drag that carries files (not selected text or a link). */
export function dragHasFiles(data: Pick<DataTransfer, "types"> | null | undefined): boolean {
  return Array.from(data?.types ?? []).includes("Files");
}
