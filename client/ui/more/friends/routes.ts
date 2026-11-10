/** The Friends pages (Labs), pushed on the More stack. */
export type FriendsPage =
  | { name: "friends" }
  | { name: "friends-post"; id: string }
  | { name: "friends-person"; id: string }
  | { name: "friends-drafts" }
  | { name: "friends-notifications" }
  | { name: "friends-bookmarks" }
  | { name: "friends-blocked" };
