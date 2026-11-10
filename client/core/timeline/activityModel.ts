/** From the Expo app's components/terminal/InterfaceTimelineActivityModel.ts. */
import type { ActivityTimelineItem } from "./activityTypes";
import type { WorkStatus } from "./workStatus";

/** The theme colours a tool row's tone uses; the app's TerminalThemeChrome has these among others. */
export interface ActivityToneChrome {
  accent: string;
  danger: string;
  textMuted: string;
  textSubtle: string;
}

export interface InterfaceTimelineActivityPresentation {
  canExpand: boolean;
  toneColor: string;
  /** The row's StatusMark, standing in for the leading status word. */
  statusMark: WorkStatus | null;
  /** The detail with that word removed (duration, exit code, totals). */
  detailText?: string;
}

export function buildInterfaceTimelineActivityPresentation(
  item: ActivityTimelineItem,
  chrome: ActivityToneChrome,
): InterfaceTimelineActivityPresentation {
  const status = splitActivityStatusDetail(item.detail, item.tone);
  return {
    canExpand: canExpandActivity(item),
    toneColor: activityToneColor(item, chrome),
    statusMark: status.mark,
    detailText: status.rest,
  };
}

// Leading status words written by toolResultStatusLine and the wait rows.
// "Done" and "Finished" only say the call returned, so they get no mark.
const ACTIVITY_STATUS_WORDS: Readonly<Record<string, WorkStatus | null>> = {
  running: "running",
  waiting: "running",
  succeeded: "ready",
  passed: "ready",
  failed: "failed",
  blocked: "blocked",
  done: null,
  finished: null,
};

/**
 * Seal & Slip: a tool row's status is a glyph, not a mono word. Splits a
 * collapsed detail such as "Failed · exit 1 · 2s" into the mark (failed) and
 * the cause that stays as copy ("exit 1 · 2s"). Running and failed rows keep
 * their mark even when the detail carries no status word.
 */
export function splitActivityStatusDetail(
  detail: string | undefined,
  tone: ActivityTimelineItem["tone"],
): { mark: WorkStatus | null; rest?: string } {
  const toneMark: WorkStatus | null =
    tone === "running" ? "running" : tone === "failed" ? "failed" : null;
  const trimmed = detail?.trim();
  if (!trimmed) {
    return { mark: toneMark };
  }
  const [head, ...tail] = trimmed.split(" · ");
  const word = head.trim().toLowerCase();
  if (!Object.prototype.hasOwnProperty.call(ACTIVITY_STATUS_WORDS, word)) {
    return { mark: toneMark, rest: trimmed };
  }
  const rest = tail.join(" · ").trim();
  return {
    mark: toneMark ?? ACTIVITY_STATUS_WORDS[word],
    rest: rest || undefined,
  };
}

/** Only a row that says so opens itself; a failed call starts collapsed like any other. */
export function shouldAutoExpandActivity(item: ActivityTimelineItem) {
  return item.defaultExpanded === true;
}

export function expandedActivityStatusLine(
  item: ActivityTimelineItem,
): string | undefined {
  const status = item.statusLine?.trim();
  if (!status || status === item.detail?.trim()) {
    return undefined;
  }
  return item.statusLine;
}

function canExpandActivity(item: ActivityTimelineItem) {
  return Boolean(
    item.body ||
    expandedActivityStatusLine(item) ||
    item.commandText ||
    item.queryText ||
    item.fileSummaries?.length ||
    item.files?.length ||
    item.previewPath ||
    item.children?.length ||
    item.steps?.length,
  );
}

function activityToneColor(
  item: ActivityTimelineItem,
  chrome: ActivityToneChrome,
) {
  if (item.activityKind === "reasoning") {
    return chrome.accent;
  }
  if (item.tone === "failed") {
    return chrome.danger;
  }
  if (item.tone === "running") {
    return chrome.textMuted;
  }
  if (item.tone === "success") {
    return chrome.textSubtle;
  }
  return chrome.textSubtle;
}
