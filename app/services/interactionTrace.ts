export type PrimaryRouteName = "brain" | "list";
export type DrawerTraceSource =
  | "back"
  | "close-button"
  | "escape"
  | "gesture"
  | "menu"
  | "navigation"
  | "overlay";

export interface InteractionMetadataMap {
  "primary.switch": {
    from: PrimaryRouteName;
    to: PrimaryRouteName;
  };
  "drawer.open": {
    source: DrawerTraceSource;
    target: "closed" | "open";
  };
  "drawer.close": {
    source: DrawerTraceSource;
    target: "closed" | "open";
  };
  "composer.focus": {
    source: "input";
  };
  "agent.update": {
    agent: number | string;
  };
}

export type InteractionName = keyof InteractionMetadataMap;
export type InteractionStatus = "cancelled" | "completed";

export interface InteractionRecord<Name extends InteractionName = InteractionName> {
  id: number;
  name: Name;
  metadata: InteractionMetadataMap[Name];
  status: InteractionStatus;
  startAt: number;
  activationAt?: number;
  commitAt?: number;
  afterPaintAt?: number;
  releaseAt?: number;
  endAt: number;
  durationMs: number;
}

export interface InteractionToken<Name extends InteractionName> {
  readonly id: number;
  readonly name: Name;
  markActivation(at?: number): void;
  markCommit(at?: number): void;
  markAfterPaint(at?: number): void;
  markRelease(at?: number): void;
  end(at?: number): void;
  cancel(at?: number): void;
}

export interface CompletedInteraction<Name extends InteractionName> {
  name: Name;
  metadata: InteractionMetadataMap[Name];
  startAt: number;
  activationAt?: number;
  commitAt?: number;
  afterPaintAt?: number;
  releaseAt?: number;
  endAt: number;
  cancelled?: boolean;
}

const MAX_RECORDS = 200;

export const MEWLA_INTERACTION_TRACE_ENABLED =
  typeof __DEV__ !== "undefined" &&
  __DEV__ &&
  process.env.EXPO_PUBLIC_MEWLA_INTERACTION_TRACE === "1";

let nextInteractionId = 1;
const records: InteractionRecord[] = [];

function now(): number {
  return typeof performance !== "undefined" &&
    typeof performance.now === "function"
    ? performance.now()
    : Date.now();
}

function markName(id: number, stage: string): string {
  return `mewla-interaction:${id}:${stage}`;
}

function markWeb(id: number, stage: string, at: number): void {
  if (
    typeof document === "undefined" ||
    typeof performance === "undefined" ||
    typeof performance.mark !== "function"
  ) {
    return;
  }
  try {
    performance.mark(markName(id, stage), { startTime: at });
  } catch {
    performance.mark(markName(id, stage));
  }
}

function measureWeb(id: number, name: InteractionName): void {
  if (
    typeof document === "undefined" ||
    typeof performance === "undefined" ||
    typeof performance.measure !== "function"
  ) {
    return;
  }
  try {
    performance.measure(
      `mewla-interaction:${name}:${id}`,
      markName(id, "start"),
      markName(id, "end"),
    );
  } catch {
    return;
  }
}

function storeRecord(record: InteractionRecord): void {
  records.push(record);
  if (records.length > MAX_RECORDS) {
    records.splice(0, records.length - MAX_RECORDS);
  }
}

function createNoopToken<Name extends InteractionName>(
  name: Name,
): InteractionToken<Name> {
  return {
    id: 0,
    name,
    markActivation() {},
    markCommit() {},
    markAfterPaint() {},
    markRelease() {},
    end() {},
    cancel() {},
  };
}

export function beginInteraction<Name extends InteractionName>(
  name: Name,
  metadata: InteractionMetadataMap[Name],
): InteractionToken<Name> {
  if (!MEWLA_INTERACTION_TRACE_ENABLED) {
    return createNoopToken(name);
  }

  const id = nextInteractionId;
  nextInteractionId += 1;
  const startAt = now();
  const stages: Partial<
    Record<"activation" | "afterPaint" | "commit" | "release", number>
  > = {};
  let finished = false;

  markWeb(id, "start", startAt);

  const mark = (
    stage: "activation" | "afterPaint" | "commit" | "release",
    at = now(),
  ) => {
    if (finished || stages[stage] != null) {
      return;
    }
    stages[stage] = at;
    markWeb(id, stage, at);
  };

  const finish = (status: InteractionStatus, at = now()) => {
    if (finished) {
      return;
    }
    finished = true;
    markWeb(id, "end", at);
    storeRecord({
      id,
      name,
      metadata,
      status,
      startAt,
      activationAt: stages.activation,
      commitAt: stages.commit,
      afterPaintAt: stages.afterPaint,
      releaseAt: stages.release,
      endAt: at,
      durationMs: Math.max(0, at - startAt),
    });
    measureWeb(id, name);
  };

  return {
    id,
    name,
    markActivation: (at) => mark("activation", at),
    markCommit: (at) => mark("commit", at),
    markAfterPaint: (at) => mark("afterPaint", at),
    markRelease: (at) => mark("release", at),
    end: (at) => finish("completed", at),
    cancel: (at) => finish("cancelled", at),
  };
}

export function recordCompletedInteraction<Name extends InteractionName>(
  interaction: CompletedInteraction<Name>,
): void {
  if (!MEWLA_INTERACTION_TRACE_ENABLED) {
    return;
  }
  const id = nextInteractionId;
  nextInteractionId += 1;
  markWeb(id, "start", interaction.startAt);
  if (interaction.activationAt != null) {
    markWeb(id, "activation", interaction.activationAt);
  }
  if (interaction.commitAt != null) {
    markWeb(id, "commit", interaction.commitAt);
  }
  if (interaction.afterPaintAt != null) {
    markWeb(id, "afterPaint", interaction.afterPaintAt);
  }
  if (interaction.releaseAt != null) {
    markWeb(id, "release", interaction.releaseAt);
  }
  markWeb(id, "end", interaction.endAt);
  storeRecord({
    id,
    name: interaction.name,
    metadata: interaction.metadata,
    status: interaction.cancelled ? "cancelled" : "completed",
    startAt: interaction.startAt,
    activationAt: interaction.activationAt,
    commitAt: interaction.commitAt,
    afterPaintAt: interaction.afterPaintAt,
    releaseAt: interaction.releaseAt,
    endAt: interaction.endAt,
    durationMs: Math.max(0, interaction.endAt - interaction.startAt),
  });
  measureWeb(id, interaction.name);
}

export function snapshotInteractionTraces(): readonly InteractionRecord[] {
  if (!MEWLA_INTERACTION_TRACE_ENABLED) {
    return [];
  }
  return records.map((record) => ({ ...record }));
}

export function drainInteractionTraces(): readonly InteractionRecord[] {
  if (!MEWLA_INTERACTION_TRACE_ENABLED) {
    return [];
  }
  return records.splice(0, records.length);
}
