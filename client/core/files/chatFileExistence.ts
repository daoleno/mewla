/**
 * Which of the paths Chat replies name are files on the speaking Session's computer (`chatPaths.ts`: a path that
 * is not a home's is linked only once it is known to be one). One cache per Session run (`scope`: server, Session,
 * process, start), asked in batches: the paths rows ask about in one tick go in one request, each path once.
 *
 * An answer is kept for the run; a request that failed leaves its paths unknown, asked again the next time a row
 * asks. A computer that does not know the request (an older daemon) is answered "none" for the run.
 */

export type AskExisting = (paths: string[]) => Promise<string[]>;

/** A refusal that will not change for this run: the computer has no such request. */
export type IsFinalRefusal = (error: unknown) => boolean;

const MAX_SCOPES = 8;
const MAX_PATHS_PER_SCOPE = 4000;
const BATCH = 64;

type Scope = {
  known: Map<string, boolean>;
  asking: Set<string>;
  queued: Set<string>;
  ask: AskExisting;
  refused: boolean;
};

export type ChatFileExistence = {
  /**
   * The paths of `paths` known to be files in `scope`; the unknown ones are asked about (with `ask`), and
   * `onChange` runs once an answer added any.
   */
  existing(scope: string, paths: readonly string[], ask: AskExisting): ReadonlySet<string>;
  /** Forgets every answer: the current server changed. */
  clear(): void;
};

export function createChatFileExistence(options: {
  onChange: () => void;
  isFinalRefusal?: IsFinalRefusal;
  /** When a batch is sent; a microtask by default. */
  schedule?: (run: () => void) => void;
}): ChatFileExistence {
  const scopes = new Map<string, Scope>();
  const schedule = options.schedule ?? ((run) => queueMicrotask(run));
  let generation = 0;

  const scopeOf = (key: string, ask: AskExisting): Scope => {
    let scope = scopes.get(key);
    if (scope) {
      scopes.delete(key);
      scopes.set(key, scope); // most recent last
      scope.ask = ask;
      return scope;
    }
    scope = { known: new Map(), asking: new Set(), queued: new Set(), ask, refused: false };
    scopes.set(key, scope);
    while (scopes.size > MAX_SCOPES) scopes.delete(scopes.keys().next().value as string);
    return scope;
  };

  const flush = (key: string, scope: Scope) => {
    const paths = [...scope.queued];
    scope.queued.clear();
    const asked = generation;
    for (let i = 0; i < paths.length; i += BATCH) {
      const batch = paths.slice(i, i + BATCH);
      for (const path of batch) scope.asking.add(path);
      void scope.ask(batch).then(
        (found) => {
          if (asked !== generation || scopes.get(key) !== scope) return;
          const files = new Set(found);
          let added = false;
          for (const path of batch) {
            scope.asking.delete(path);
            const exists = files.has(path);
            scope.known.set(path, exists);
            added ||= exists;
          }
          trim(scope);
          if (added) options.onChange();
        },
        (error: unknown) => {
          if (asked !== generation || scopes.get(key) !== scope) return;
          for (const path of batch) scope.asking.delete(path);
          if (options.isFinalRefusal?.(error)) scope.refused = true;
        },
      );
    }
  };

  return {
    existing(key, paths, ask) {
      if (!key || !paths.length) return EMPTY;
      const scope = scopeOf(key, ask);
      const found = new Set<string>();
      if (scope.refused) return found;
      const wasQueued = scope.queued.size > 0;
      for (const path of paths) {
        const known = scope.known.get(path);
        if (known) found.add(path);
        else if (known === undefined && !scope.asking.has(path)) scope.queued.add(path);
      }
      if (!wasQueued && scope.queued.size) schedule(() => flush(key, scope));
      return found;
    },
    clear() {
      generation++;
      scopes.clear();
    },
  };
}

const EMPTY: ReadonlySet<string> = new Set();

function trim(scope: Scope): void {
  while (scope.known.size > MAX_PATHS_PER_SCOPE) scope.known.delete(scope.known.keys().next().value as string);
}
