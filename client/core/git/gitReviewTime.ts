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

/** The age as a screen reader says it: "just now", "5 minutes ago", "3 weeks ago" (the short one reads "now ago"). */
export function spokenGitAge(unixSeconds: number, now: number = Date.now()): string {
  const short = formatGitAge(unixSeconds, now);
  if (short === "now") return "just now";
  const match = /^(\d+)(mo|m|h|d|w|y)$/.exec(short);
  if (!match) return short;
  const count = Number(match[1]);
  const unit = { m: "minute", h: "hour", d: "day", w: "week", mo: "month", y: "year" }[match[2]]!;
  return `${count} ${unit}${count === 1 ? "" : "s"} ago`;
}
