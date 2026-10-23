import { serverAddressForDisplay } from "./storedServerContract";
import type { StoredServer } from "./storedServerContract";

/** The deep link a live notification, chip or widget opens: mewla://live/answer?q=<question>&c=<choice index>. */
export interface LiveAnswerTarget {
  questionId: string;
  choiceIndex: number;
}

/** One request id per question and choice, so a re-delivered tap is the same answer to the daemon. */
export function liveAnswerRequestId(target: LiveAnswerTarget): string {
  return `live:${target.questionId}:${target.choiceIndex}`;
}

export function parseLiveAnswerParams(
  questionId: string | undefined,
  choice: string | undefined,
): LiveAnswerTarget | null {
  const id = questionId?.trim() ?? "";
  if (!id || choice === undefined || !/^[0-3]$/.test(choice.trim())) return null;
  return { questionId: id, choiceIndex: Number(choice.trim()) };
}

/**
 * Maps the live deep links to routes the app has: answering opens Brain with the answer
 * carried as one-shot params, anything else under live/ opens Brain. Other links pass through.
 */
export function redirectLivePath(path: string): string {
  let parsed: URL;
  try {
    parsed = new URL(path, "mewla://app");
  } catch {
    return path;
  }
  if (parsed.protocol !== "mewla:" || parsed.host !== "live") return path;
  const target = parseLiveAnswerParams(
    parsed.searchParams.get("q") ?? undefined,
    parsed.searchParams.get("c") ?? undefined,
  );
  if (parIs(parsed.pathname, "/answer") && target) {
    return `/?liveAnswer=${encodeURIComponent(target.questionId)}&liveChoice=${target.choiceIndex}`;
  }
  return "/";
}

function parIs(pathname: string, expected: string): boolean {
  return pathname.replace(/\/+$/, "") === expected;
}

/**
 * Where the widget fetches its snapshot. Only a plain address works: a relay (link)
 * transport has no URL a widget can reach, and says so by returning null.
 */
export function widgetBaseUrl(server: Pick<StoredServer, "url" | "transportKind">): string | null {
  if (server.transportKind === "link") return null;
  const address = serverAddressForDisplay(server.url);
  return /^https?:\/\//.test(address) ? address : null;
}
