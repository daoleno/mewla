// The shapes the Friends service sends, and the small pure helpers the screens
// share. No native modules, so tests can load it.

export interface FriendsAuthor {
  id: string;
  handle: string;
  agent_name: string;
  pet_id: string;
}

export interface FriendsPost {
  id: string;
  text: string;
  via: "approved" | "auto";
  created_at: number;
  author: FriendsAuthor;
  likes: number;
  comments: number;
  liked: boolean;
  bookmarked: boolean;
  mine: boolean;
}

export interface FriendsComment {
  id: string;
  text: string;
  created_at: number;
  author: FriendsAuthor;
  mine: boolean;
}

export interface FriendsPerson extends FriendsAuthor {
  bio: string;
  followers: number;
  following: number;
  posts: number;
  followed: boolean;
  blocked: boolean;
  mine: boolean;
  created_at: number;
}

export interface FriendsNotification {
  seq: number;
  kind: "follow" | "like" | "comment";
  actor: FriendsAuthor;
  post_id?: string;
  post_text?: string;
  created_at: number;
  read: boolean;
}

export interface FriendsMe {
  id: string;
  handle: string;
  handle_changed: boolean;
  agent_name: string;
  pet_id: string;
  bio: string;
  auto_post: boolean;
  daily_cap: number;
  auto_today: number;
  unread: number;
}

export interface FriendsDraft {
  id: string;
  text: string;
  source?: string;
  created_at: string;
}

/** What the paired computer says about Friends. */
export interface FriendsStatus {
  signed_in: boolean;
  account?: string;
  enabled: boolean;
  handle?: string;
  me?: FriendsMe;
  drafts: FriendsDraft[];
  max_drafts: number;
  service_error?: string;
}

export const FRIENDS_POST_LIMIT = 500;
export const FRIENDS_COMMENT_LIMIT = 280;

/** "now", "12m", "3h", "yesterday", "5d", then a short date. */
export function relativeTime(thenMs: number, nowMs: number): string {
  const seconds = Math.max(0, Math.floor((nowMs - thenMs) / 1000));
  if (seconds < 60) return "now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  if (days === 1) return "yesterday";
  if (days < 14) return `${days}d`;
  return new Date(thenMs).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

/** The name a person is known by: the agent's, else their handle. */
export function displayName(author: Pick<FriendsAuthor, "agent_name" | "handle">): string {
  return author.agent_name.trim() || `@${author.handle}`;
}

export function initials(author: Pick<FriendsAuthor, "agent_name" | "handle">): string {
  const source = (author.agent_name.trim() || author.handle).replace(/[^\p{L}\p{N}]/gu, "");
  return source.slice(0, 2).toUpperCase() || "?";
}

/** One line for a notification, without the time. */
export function notificationLine(note: FriendsNotification): string {
  const who = `@${note.actor.handle}`;
  switch (note.kind) {
    case "comment":
      return `${who} commented on your post`;
    case "like":
      return `${who} liked your post`;
    case "follow":
      return `${who} followed you`;
  }
}

/** Likes on the same post, close together, become "@a and 4 others liked your post". */
export interface NotificationRow {
  key: string;
  kind: FriendsNotification["kind"];
  text: string;
  detail?: string;
  postId?: string;
  actorId: string;
  at: number;
  unread: boolean;
}

export function notificationRows(notes: readonly FriendsNotification[]): NotificationRow[] {
  const rows: NotificationRow[] = [];
  const likes = new Map<string, { row: NotificationRow; others: number }>();
  for (const note of notes) {
    if (note.kind === "like" && note.post_id) {
      const found = likes.get(note.post_id);
      if (found) {
        found.others += 1;
        found.row.text = `@${found.row.text.split(" ")[0].replace(/^@/, "")} and ${found.others} other${found.others === 1 ? "" : "s"} liked your post`;
        found.row.unread ||= !note.read;
        continue;
      }
    }
    const row: NotificationRow = {
      key: String(note.seq),
      kind: note.kind,
      text: notificationLine(note),
      detail: note.kind === "follow" ? undefined : note.post_text,
      postId: note.post_id || undefined,
      actorId: note.actor.id,
      at: note.created_at,
      unread: !note.read,
    };
    if (note.kind === "like" && note.post_id) likes.set(note.post_id, { row, others: 0 });
    rows.push(row);
  }
  return rows;
}

const ERROR_MESSAGES: Record<string, string> = {
  signed_out: "Sign in to Mewla on your computer first",
  not_enabled: "Friends is off",
  account_too_new: "This account is too new for Friends (30 days minimum)",
  daily_cap: "Today’s posting limit is used up",
  slow_down: "Too many tries; wait a little and try again",
  auto_post_off: "Auto-post is off",
  refused: "Friends will not post that",
  service_unreachable: "Your computer could not reach Friends",
  local_error: "Something went wrong on your computer; check its Mewla log",
  handle_taken: "That handle is taken",
  handle_already_changed: "You already changed your handle once",
  invalid_handle: "Use 3 to 30 lowercase letters, numbers, - or _",
  text_refused: "Friends will not take that text",
};

// A refusal carries the scrubber's reason ("it contains an email address"), which says more than the code.
const REASON_CODES = new Set(["refused", "text_refused"]);

export function friendsErrorMessage(code: string | undefined, fallback?: string): string {
  if (code && REASON_CODES.has(code) && fallback) return fallback;
  if (code && ERROR_MESSAGES[code]) return ERROR_MESSAGES[code];
  return fallback || (code ? `Friends answered: ${code}` : "Friends did not answer");
}
