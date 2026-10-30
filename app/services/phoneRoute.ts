/**
 * The link Phone use's notifications open (mewla://phone). It is rewritten to the route's own
 * path like the live links are: a running app does not route the bare scheme link by itself.
 * Matched as text: the URL class on a phone does not implement every part of a URL.
 */
export function redirectPhonePath(path: string): string {
  return /^mewla:\/\/phone\/?(?:[?#].*)?$/.test(path.trim()) ? "/phone" : path;
}
