import type { Daemon } from "../more/daemon";

/** What the collector holds and did (the daemon's `holds` in resource telemetry). */
export interface ResourceHolds {
  /** The day of the size sample, YYYY-MM-DD; absent before the first. */
  day?: string;
  kinds: HoldKind[];
  owners: HoldOwner[];
  kept: HeldKeep[];
  today: { items: number; bytes: number; restorable: HoldsCollected[] };
}

export interface HoldKind {
  kind: string;
  count: number;
  bytes: number;
  oldest?: number;
  /** Bytes now minus bytes 7 days before, when that day was sampled. */
  growth7d?: number;
}

export interface HoldOwner {
  unit: string;
  sessionId?: string;
  name?: string;
  workId?: string;
  workTitle?: string;
  bytes: number;
}

export interface HeldKeep {
  id: string;
  path: string;
  sessionId?: string;
  until?: number;
  why: string;
}

export interface HoldsCollected {
  id: string;
  kind: string;
  original: string;
  bytes: number;
  reason: string;
  at?: number;
}

type Raw = Record<string, unknown>;

const record = (value: unknown): Raw | null => (value && typeof value === "object" && !Array.isArray(value) ? (value as Raw) : null);
const num = (value: unknown): number => (typeof value === "number" && Number.isFinite(value) ? value : 0);
const str = (value: unknown): string | undefined => (typeof value === "string" && value.trim() ? value.trim() : undefined);
const time = (value: unknown): number | undefined => {
  if (typeof value !== "string") return undefined;
  const at = Date.parse(value);
  return Number.isFinite(at) && at > 0 ? at : undefined;
};
const list = (value: unknown): Raw[] => (Array.isArray(value) ? value.map(record).filter((r): r is Raw => r !== null) : []);

export function normalizeHolds(value: unknown): ResourceHolds | undefined {
  const source = record(value);
  if (!source) return undefined;
  const today = record(source.collected_today) ?? {};
  return {
    day: str(source.day),
    kinds: list(source.kinds).flatMap((k) => {
      const kind = str(k.kind);
      return kind ? [{ kind, count: num(k.count), bytes: num(k.bytes), oldest: time(k.oldest), growth7d: typeof k.growth_7d === "number" ? k.growth_7d : undefined }] : [];
    }),
    owners: list(source.owners).flatMap((o) => {
      const unit = str(o.unit);
      return unit ? [{ unit, sessionId: str(o.session_id), name: str(o.name), workId: str(o.work_id), workTitle: str(o.work_title), bytes: num(o.bytes) }] : [];
    }),
    kept: list(source.kept).flatMap((k) => {
      const id = str(k.id);
      const path = str(k.path);
      return id && path ? [{ id, path, sessionId: str(k.session_id), until: time(k.until), why: str(k.why) ?? "" }] : [];
    }),
    today: {
      items: num(today.items),
      bytes: num(today.bytes),
      restorable: list(today.restorable).flatMap((c) => {
        const id = str(c.id);
        const original = str(c.original);
        return id && original ? [{ id, kind: str(c.kind) ?? "", original, bytes: num(c.bytes), reason: str(c.reason) ?? "", at: time(c.at) }] : [];
      }),
    },
  };
}

const KIND_LABEL: Record<string, string> = {
  scratch: "Session scratch",
  unmarked: "Unclaimed scratch",
  review: "To review",
  trash: "Trash",
  kept: "Kept",
  cache: "Shared cache",
};

export function holdKindLabel(kind: string): string {
  return KIND_LABEL[kind] ?? kind;
}

/** "+1.2 GB in 7 days", "−300 MB in 7 days", "Same as 7 days ago"; undefined without a sample then. */
export function growthText(bytes: number | undefined, format: (bytes: number) => string): string | undefined {
  if (bytes === undefined) return undefined;
  if (bytes === 0) return "Same as 7 days ago";
  return `${bytes > 0 ? "+" : "−"}${format(Math.abs(bytes))} in 7 days`;
}

/** An owner by its Work, else its Session. */
export function holdOwnerTitle(owner: HoldOwner): string {
  return owner.workTitle ?? owner.name ?? owner.sessionId ?? "Ended Session";
}

export function baseName(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  return trimmed.slice(Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\")) + 1) || trimmed;
}

/** Puts an item collected today back where it was. */
export async function restoreCollected(daemon: Daemon, id: string): Promise<void> {
  await daemon.request("gc_restore", { id }, { reply: "gc_restored", timeoutMs: 10_000 });
}
