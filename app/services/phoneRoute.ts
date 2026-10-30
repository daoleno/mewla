/**
 * The link Phone use's notifications open (mewla://phone). It is rewritten to the route's own
 * path like the live links are: a running app does not route the bare scheme link by itself.
 */
export function redirectPhonePath(path: string): string {
  let parsed: URL;
  try {
    parsed = new URL(path, "mewla://app");
  } catch {
    return path;
  }
  if (parsed.protocol !== "mewla:" || parsed.host !== "phone") return path;
  return "/phone";
}
