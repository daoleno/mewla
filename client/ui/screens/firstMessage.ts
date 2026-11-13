import { Storage } from "nsh";
import { chatCacheKey, Draft } from "../../core/conversation/draft";
import { firstMessageNews } from "../../core/sessions/firstMessage";
import { writeIntoDraft } from "../chat/Composer";
import { onTransport, outbox } from "../session";

/**
 * A new Session's first message while the computer holds it (WIRE §11), by computer and Session. The computer
 * keeps no copy, so the text stays here until the computer says the agent took it: anything else gives it back
 * as that Session's draft, where the person sees it and decides (it is never sent again by itself).
 */
const held = new Map<string, Map<string, { text: string; entry: string }>>();

/** Puts `text` in the Session's composer draft (an open composer shows it at once). */
function giveBack(serverId: string, workerId: string, text: string): void {
  if (text.trim()) writeIntoDraft(new Draft(Storage, chatCacheKey(serverId, workerId)), text);
}

/**
 * The New session sheet started `workerId` with `text`; `pending` when the computer holds it, else it comes back now.
 * Held, it is the Session's first message, sending (SPEC §16.4 Sending), and what the person sends meanwhile queues
 * behind it in the outbox, in order, as messages do while the computer is away.
 */
export function startedWithFirstMessage(serverId: string, workerId: string, text: string, pending: boolean): void {
  if (!text.trim()) return;
  if (!pending) return giveBack(serverId, workerId, text);
  const forServer = held.get(serverId) ?? new Map<string, { text: string; entry: string }>();
  forServer.set(workerId, { text, entry: outbox.hold({ serverId, workerId, text }).id });
  held.set(serverId, forServer);
}

onTransport((transport, server) => {
  const backAll = () => {
    const forServer = held.get(server.id);
    if (!forServer) return;
    held.delete(server.id);
    for (const [workerId, { text, entry }] of forServer) {
      outbox.release(entry, "withdrawn");
      giveBack(server.id, workerId, text);
    }
  };
  const stops = [
    transport.on("session_first_message", (frame) => {
      const news = firstMessageNews(frame);
      if (!news) return;
      const forServer = held.get(server.id);
      const mine = forServer?.get(news.workerId);
      const text = mine?.text ?? news.text;
      forServer?.delete(news.workerId);
      if (mine) outbox.release(mine.entry, news.outcome === "sent" ? "sent" : "withdrawn");
      if (news.outcome !== "sent") giveBack(server.id, news.workerId, text);
    }),
    // A connection that dropped may mean a computer that restarted and lost what it held: give it back.
    transport.on("disconnected", backAll),
  ];
  return () => {
    for (const stop of stops) stop();
    backAll();
  };
});
