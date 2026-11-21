/**
 * The refs a commit carries (`git log %D`: "HEAD -> feat/x", "origin/feat/x", "tag: v1.2"), ready to draw as chips on a
 * line of their own under the commit's text: HEAD is its own small marker, local branches and tags are chips, remote
 * branches are quieter chips, and a long name keeps its start and its end.
 */
export type CommitRefKind = "local" | "tag" | "remote";

export interface CommitRefChip {
  /** What the chip says: the name, cut in the middle when it is long. */
  label: string;
  /** The whole name (for a screen reader). */
  name: string;
  kind: CommitRefKind;
}

export interface CommitRefs {
  /** The commit is the checked-out one. */
  head: boolean;
  chips: CommitRefChip[];
  /** Chips left out past `limit`. */
  more: number;
}

/** Remotes named by convention; the branch's own upstream (`origin/main`) adds its remote. */
const COMMON_REMOTES = ["origin", "upstream"];

/** `feat/globalresearch-source-eu` at 18 → `feat/glob…rce-eu`: the start says what kind, the end says which. */
export function middleEllipsis(text: string, max = 22): string {
  const chars = [...text];
  if (chars.length <= max) return text;
  const keep = max - 1;
  const tail = Math.ceil(keep * 0.4);
  const head = keep - tail;
  return `${chars.slice(0, head).join("")}…${chars.slice(chars.length - tail).join("")}`;
}

export function parseCommitRefs(refs: readonly string[] | undefined, options: { upstream?: string; limit?: number; max?: number } = {}): CommitRefs {
  const remotes = new Set(COMMON_REMOTES);
  const upstreamRemote = options.upstream?.split("/")[0];
  if (upstreamRemote) remotes.add(upstreamRemote);
  let head = false;
  const local: CommitRefChip[] = [];
  const tags: CommitRefChip[] = [];
  const remote: CommitRefChip[] = [];
  const chip = (name: string, kind: CommitRefKind): CommitRefChip => ({ label: middleEllipsis(name, options.max), name, kind });
  for (const raw of refs ?? []) {
    const ref = raw.trim();
    if (!ref) continue;
    if (ref === "HEAD") {
      head = true;
    } else if (ref.startsWith("HEAD -> ")) {
      head = true;
      local.push(chip(ref.slice("HEAD -> ".length), "local"));
    } else if (ref.startsWith("tag: ")) {
      tags.push(chip(ref.slice("tag: ".length), "tag"));
    } else if (remotes.has(ref.split("/")[0]!) && ref.includes("/")) {
      // A remote's own HEAD (origin/HEAD) says nothing a person acts on.
      if (!ref.endsWith("/HEAD")) remote.push(chip(ref, "remote"));
    } else {
      local.push(chip(ref, "local"));
    }
  }
  const all = [...local, ...tags, ...remote];
  const limit = options.limit ?? 6;
  return { head, chips: all.slice(0, limit), more: Math.max(0, all.length - limit) };
}
