import type { FriendsPost } from "./api";

/** One page of posts from the service; `next` is the cursor of the following page, "" at the end. */
export interface PostPage {
  posts: FriendsPost[];
  next: string;
}

/** The list's path for the page after `before` (the service's `before=<cursor>`). */
export function withCursor(path: string, before: string): string {
  if (!before) return path;
  return `${path}${path.includes("?") ? "&" : "?"}before=${encodeURIComponent(before)}`;
}

/** A following page appended: a post already shown (it moved while paging) stays where it was. */
export function appendPage(shown: readonly FriendsPost[], page: readonly FriendsPost[]): FriendsPost[] {
  const seen = new Set(shown.map((post) => post.id));
  return [...shown, ...page.filter((post) => !seen.has(post.id))];
}

/** The service's answer to a like or bookmark replaces the post; `{ok:true}` keeps the shown one. */
export function replacePost(posts: readonly FriendsPost[], post: FriendsPost): FriendsPost[] {
  return posts.map((item) => (item.id === post.id ? post : item));
}

/** What a tap on like or bookmark shows at once, before the service answers. */
export function toggled(post: FriendsPost, field: "like" | "bookmark"): FriendsPost {
  return field === "like" ? { ...post, liked: !post.liked, likes: Math.max(0, post.likes + (post.liked ? -1 : 1)) } : { ...post, bookmarked: !post.bookmarked };
}
