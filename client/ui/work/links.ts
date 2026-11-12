import type { WorkLinks } from "./WorkPage";

/** Where Work detail leads outside itself; the notifications module sets it at start. */
export const workLinks: WorkLinks = { openSession: () => {}, openCalendarItem: () => {}, askBrain: () => {} };
