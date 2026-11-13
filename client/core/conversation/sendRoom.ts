/**
 * A send places the sent message at the top of what the thread shows, below a capsule header, with the reply
 * under it (the owner's rule of 11-13). The list scrolls the sent row to its top; but a newest row cannot reach the
 * top of a list with nothing below it (the scroll stops at the content's end), so the thread ends in room: as high
 * as the list at first, less what the reply has grown into it since, and gone once the reply fills the list.
 * Without it Brain's send "end-followed" (Mac re-run 11-14, 5d).
 */

/** A row of the thread, as far as finding the sent one goes. */
export type SentRowCandidate = { type: string; role?: string; pending?: boolean; timestamp?: string };

/** How long before the send a sent row's clock may say it was made (the row is made as the send starts). */
const SENT_SLACK_MS = 2_000;

/**
 * The index of the row the send made: the newest of the person's messages still on its way that was made at the
 * send or after. -1 while it is not in the rows yet (a thread's rows can follow its send a turn later); an older
 * message that failed to go out is never it.
 */
export function sentRowIndex(rows: readonly SentRowCandidate[], sentAt: number): number {
  for (let index = rows.length - 1; index >= 0; index -= 1) {
    const row = rows[index];
    if (row.type !== "message" || row.role !== "user" || !row.pending) continue;
    const made = row.timestamp ? Date.parse(row.timestamp) : Number.NaN;
    if (Number.isFinite(made) && made >= sentAt - SENT_SLACK_MS) return index;
  }
  return -1;
}

/**
 * The room left: what still lies between the room's top (the end of the rows, where it starts) and the list's
 * bottom, never more than it was (a scroll down into the room must not grow it), never negative.
 */
export function roomLeft(current: number, listBottom: number, roomTop: number): number {
  return Math.max(0, Math.min(current, listBottom - roomTop));
}

/** How far a list is from its newest row, its room left out (the room is not content the person has not read). */
export function fromNewest(contentHeight: number, room: number, offset: number, viewport: number): number {
  return contentHeight - room - (offset + viewport);
}
