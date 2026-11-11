/**
 * The one Friends video playing: a post's, in one list or page (`owner`). One plays at
 * a time; another one starting stops it, and so does its row leaving the screen.
 */
export interface PlayingVideo {
  owner: string;
  postId: string;
}

export function isPlaying(playing: PlayingVideo | null, owner: string, postId: string): boolean {
  return playing !== null && playing.owner === owner && playing.postId === postId;
}

/** What still plays once `owner`'s list shows only `visibleIds` (each at least 40% on screen). */
export function afterScroll(playing: PlayingVideo | null, owner: string, visibleIds: readonly string[]): PlayingVideo | null {
  if (!playing || playing.owner !== owner) return playing;
  return visibleIds.includes(playing.postId) ? playing : null;
}

/** What still plays once `owner` stops `postId` (it ended, its page went under another, the list was dropped). */
export function stopped(playing: PlayingVideo | null, owner: string, postId?: string): PlayingVideo | null {
  if (!playing || playing.owner !== owner) return playing;
  return postId === undefined || playing.postId === postId ? null : playing;
}

/** How much of a row must show for its video to keep playing. */
export const VIDEO_VISIBLE_SHARE = 0.4;
