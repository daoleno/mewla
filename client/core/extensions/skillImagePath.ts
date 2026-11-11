/**
 * The file of the same copy a link in a Skill file points to (`./reference.md`,
 * `docs/setup.md#usage`), or null for anything outside the copy (another site,
 * an absolute path, a file it doesn't have, an anchor in the same file).
 */
export function skillFileLink(url: string, markdownPath: string, files: readonly { path: string }[]): string | null {
  const reference = url.trim();
  if (!reference || reference.startsWith("#")) return null;
  try {
    return skillImagePath(reference, markdownPath, files);
  } catch {
    return null;
  }
}

/** Resolve only within the file list of the already-authorized package copy. */
export function skillImagePath(reference: string, markdownPath: string, files: readonly { path: string }[]): string {
  if (reference.startsWith("skill-file:")) {
    const exact = reference.slice("skill-file:".length);
    if (files.some((file) => file.path === exact)) return exact;
    throw new Error("The Skill image is no longer in this copy.");
  }
  if (reference.startsWith("/") || /^[a-z][a-z0-9+.-]*:/i.test(reference)) throw new Error("Image is outside this Skill copy.");
  const segments = markdownPath.split("/").slice(0, -1);
  for (const part of decodeURIComponent(reference.split(/[?#]/, 1)[0]).split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") { if (!segments.length) throw new Error("Image is outside this Skill copy."); segments.pop(); }
    else segments.push(part);
  }
  const path = segments.join("/");
  if (!files.some((file) => file.path === path)) throw new Error("Image is not in this Skill copy.");
  return path;
}
