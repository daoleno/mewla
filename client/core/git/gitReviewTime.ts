/** `now`, `5m`, `3h`, `2d`, `6w`, `4mo`, `2y`: how long ago a commit was, in a few characters. */
export function formatGitAge(unixSeconds: number, now: number = Date.now()): string {
  const seconds = Math.max(0, Math.floor(now / 1000 - unixSeconds));
  if (seconds < 60) return "now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  if (days < 14) return `${days}d`;
  if (days < 60) return `${Math.floor(days / 7)}w`;
  if (days < 365) return `${Math.floor(days / 30)}mo`;
  return `${Math.floor(days / 365)}y`;
}
