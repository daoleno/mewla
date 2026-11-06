import type { SemanticActionKind } from "../../services/toolCallSemantics";
import type { TimelineItem } from "./InterfaceTimelineItemView";
import {
  shouldAutoExpandActivity,
  splitActivityStatusDetail,
} from "./InterfaceTimelineActivityModel";
import type { ActivityTimelineItem } from "./InterfaceTimelineActivityTypes";
import { PROVIDER_ACTIVITY_ITEM_PREFIX } from "./InterfaceTimelineModel";

/** Cache key for a folded run; the run's row itself keeps its first step's id. */
const TOOL_RUN_CACHE_PREFIX = "tool-run:";

type RunBucket =
  | "edit"
  | "command"
  | "read"
  | "search"
  | "image"
  | "other";

// The summary names at most this many kinds, most consequential first. Kinds
// beyond that stay in the row's step count and in the opened run.
const SUMMARY_PRIORITY: readonly RunBucket[] = [
  "edit",
  "command",
  "read",
  "search",
  "image",
  "other",
];
const MAX_NAMED_BUCKETS = 2;

function bucketOf(kind: SemanticActionKind): RunBucket {
  switch (kind) {
    case "update_files":
      return "edit";
    case "run_command":
    case "test_app":
      return "command";
    case "read_files":
      return "read";
    case "search_code":
      return "search";
    case "view_image":
      return "image";
    default:
      return "other";
  }
}

/**
 * A finished, quiet tool row that carries its kind. Anything the person has to
 * look at stays on its own: a subagent, a running or streaming row, a failure or block
 * (those open themselves), the agent's own words, the choice card and the
 * provider's live row.
 */
export function isFoldableToolRow(
  item: TimelineItem,
): item is ActivityTimelineItem & { semanticKind: SemanticActionKind } {
  if (item.type !== "activity" || !item.semanticKind) return false;
  // A subagent is a piece of work in its own right, not a step to summarise.
  if (item.semanticKind === "delegate_agent") return false;
  if (item.tone === "running" || item.tone === "failed" || item.streaming) {
    return false;
  }
  if (item.activityKind) return false;
  if (item.id.startsWith(PROVIDER_ACTIVITY_ITEM_PREFIX)) return false;
  if (item.statusKey?.startsWith("choice:")) return false;
  if (shouldAutoExpandActivity(item)) return false;
  const mark = splitActivityStatusDetail(item.detail, item.tone).mark;
  return mark === null || mark === "ready";
}

/** "Read 3 files, ran 2 commands and edited a file": what a folded run did. */
export function summarizeToolRun(steps: readonly ActivityTimelineItem[]): string {
  const counts = new Map<RunBucket, Set<string>>();
  for (const step of steps) {
    const bucket = bucketOf(step.semanticKind ?? "use_tool");
    const keys = counts.get(bucket) ?? new Set<string>();
    counts.set(bucket, keys);
    if (bucket === "edit" || bucket === "read") {
      const paths = step.fileSummaries?.length
        ? step.fileSummaries.map((file) => file.path)
        : (step.files ?? []);
      if (paths.length > 0) {
        for (const path of paths) keys.add(`file:${path}`);
        continue;
      }
    }
    const units = Math.max(1, step.children?.length ?? 0);
    for (let unit = 0; unit < units; unit += 1) {
      keys.add(`${step.id}:${unit}`);
    }
  }
  const present = SUMMARY_PRIORITY.filter((bucket) => counts.has(bucket));
  const named = present.slice(0, MAX_NAMED_BUCKETS);
  const phrases = named.map((bucket) =>
    phraseFor(bucket, counts.get(bucket)!.size),
  );
  return phrases
    .map((phrase, index) =>
      index === 0 ? phrase : phrase.charAt(0).toLowerCase() + phrase.slice(1),
    )
    .join(" and ");
}

function phraseFor(bucket: RunBucket, count: number): string {
  const many = count !== 1;
  switch (bucket) {
    case "edit":
      return `Edited ${count} ${many ? "files" : "file"}`;
    case "command":
      return `Ran ${count} ${many ? "commands" : "command"}`;
    case "read":
      return `Read ${count} ${many ? "files" : "file"}`;
    case "search":
      return many ? `Searched code ${count} times` : "Searched code";
    case "image":
      return `Viewed ${count} ${many ? "images" : "image"}`;
    default:
      return `Performed ${count} other ${many ? "actions" : "action"}`;
  }
}

export function foldedToolRun(
  steps: ActivityTimelineItem[],
): ActivityTimelineItem {
  const title = summarizeToolRun(steps);
  const last = steps[steps.length - 1];
  return {
    type: "activity",
    // The first step's id: the list keeps that row mounted (and the reading
    // anchor on it) when a lone row becomes a run, and when a run grows.
    id: steps[0].id,
    timestamp: last.timestamp ?? steps[0].timestamp,
    statusKey: "done",
    title,
    // How many calls the row stands for, quiet beside the chevron.
    detail: `${steps.length} steps`,
    tone: "neutral",
    icon: "layers",
    defaultExpanded: false,
    accessibilityLabel: title,
    steps,
  };
}

export type ToolRunFolder = (items: TimelineItem[]) => TimelineItem[];

/**
 * Folds each turn's finished tool rows into one summary row that opens onto
 * the real rows. Runs end at anything else (words, plans, a running or failed
 * call), so a run never crosses a turn. One row stays as it is.
 *
 * The folder keeps the group it built for a run while that run's rows are the
 * same objects, so a streaming chunk elsewhere in a long history re-renders
 * no group.
 */
export function createToolRunFolder(): ToolRunFolder {
  let built = new Map<
    string,
    { steps: ActivityTimelineItem[]; group: ActivityTimelineItem }
  >();
  // A subagent's own steps fold the same way. Each subagent keeps its folder so
  // its groups survive a new row object for the same subagent, and its row
  // while the source row is the same object.
  let subagents = new Map<string, SubagentEntry>();
  return (items) => {
    const next = new Map<
      string,
      { steps: ActivityTimelineItem[]; group: ActivityTimelineItem }
    >();
    const nextSubagents = new Map<string, SubagentEntry>();
    const out: TimelineItem[] = [];
    let run: ActivityTimelineItem[] = [];
    let changed = false;
    const flush = () => {
      if (run.length < 2) {
        out.push(...run);
      } else {
        const id = `${TOOL_RUN_CACHE_PREFIX}${run[0].id}`;
        const previous = built.get(id);
        const entry =
          previous && sameSteps(previous.steps, run)
            ? previous
            : { steps: run, group: foldedToolRun(run) };
        next.set(id, entry);
        out.push(entry.group);
        changed = true;
      }
      run = [];
    };
    for (const item of items) {
      if (isFoldableToolRow(item)) {
        run.push(item);
        continue;
      }
      flush();
      if (
        item.type === "activity" &&
        item.semanticKind === "delegate_agent" &&
        item.steps?.length
      ) {
        const previous = subagents.get(item.id);
        const entry: SubagentEntry =
          previous && previous.source === item
            ? previous
            : foldSubagent(item, previous?.fold ?? createToolRunFolder());
        nextSubagents.set(item.id, entry);
        out.push(entry.row);
        if (entry.row !== item) changed = true;
        continue;
      }
      out.push(item);
    }
    flush();
    built = next;
    subagents = nextSubagents;
    return changed ? out : items;
  };
}

type SubagentEntry = {
  source: ActivityTimelineItem;
  row: ActivityTimelineItem;
  fold: ToolRunFolder;
};

function foldSubagent(
  source: ActivityTimelineItem,
  fold: ToolRunFolder,
): SubagentEntry {
  const folded = fold(source.steps ?? []) as ActivityTimelineItem[];
  return {
    source,
    fold,
    row: folded === source.steps ? source : { ...source, steps: folded },
  };
}

function sameSteps(
  left: readonly ActivityTimelineItem[],
  right: readonly ActivityTimelineItem[],
): boolean {
  return (
    left.length === right.length &&
    left.every((step, index) => step === right[index])
  );
}
