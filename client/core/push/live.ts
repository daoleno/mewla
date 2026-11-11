import { serverAddressForDisplay, type StoredServer } from "../storedServerContract";
import type { ParsedLink } from "./links";

/** The answer a live notification's chip or a widget carries: `mewla://live/answer?q=<question>&c=<choice 0-3>`. */
export interface LiveAnswer {
  questionId: string;
  choiceIndex: number;
}

export function liveAnswerOf(link: ParsedLink): LiveAnswer | null {
  if (link.section !== "live" || link.path[0] !== "answer") return null;
  const questionId = (link.params.q ?? "").trim();
  const choice = (link.params.c ?? "").trim();
  if (!questionId || !/^[0-3]$/.test(choice)) return null;
  return { questionId, choiceIndex: Number(choice) };
}

/** One request id per question and choice: a tap delivered twice is the same answer to the daemon. */
export function liveAnswerRequestId(answer: LiveAnswer): string {
  return `live:${answer.questionId}:${answer.choiceIndex}`;
}

/**
 * Where a widget fetches its snapshot: only a plain address works. A relay (Mewla Relay)
 * has no URL a widget can reach; those widgets show the states the app and pushes apply.
 */
export function widgetBaseUrl(server: Pick<StoredServer, "url" | "transportKind">): string | null {
  if (server.transportKind === "link") return null;
  const address = serverAddressForDisplay(server.url);
  return /^https?:\/\//.test(address) ? address : null;
}
