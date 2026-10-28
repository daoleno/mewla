import AsyncStorage from "@react-native-async-storage/async-storage";

/**
 * Composer drafts that survive a reload (browser storage on web, the app's
 * AsyncStorage on a phone). One entry per conversation, written shortly after
 * typing stops and removed the moment the draft is empty, which is what a send
 * leaves behind. A draft nobody came back to for two weeks is dropped.
 */
export interface DraftStorage {
  getItem(key: string): Promise<string | null>;
  setItem(key: string, value: string): Promise<void>;
  removeItem(key: string): Promise<void>;
}

const PREFIX = "mewla.draft.v1:";
export const DRAFT_MAX_AGE_MS = 14 * 24 * 60 * 60 * 1000;
export const DRAFT_WRITE_DELAY_MS = 400;

export function createDraftStore(
  storage: DraftStorage,
  { now = Date.now, delayMs = DRAFT_WRITE_DELAY_MS }: { now?: () => number; delayMs?: number } = {},
) {
  const timers = new Map<string, ReturnType<typeof setTimeout>>();

  const flush = async (key: string, value: string) => {
    try {
      if (value) await storage.setItem(PREFIX + key, JSON.stringify({ t: now(), v: value }));
      else await storage.removeItem(PREFIX + key);
    } catch {
      // A draft is a convenience; storage being full or blocked is not an error.
    }
  };

  return {
    /** Remember the draft after typing pauses; an empty draft is removed at once. */
    save(key: string, value: string) {
      const pending = timers.get(key);
      if (pending) clearTimeout(pending);
      if (!value) {
        timers.delete(key);
        void flush(key, "");
        return;
      }
      timers.set(
        key,
        setTimeout(() => {
          timers.delete(key);
          void flush(key, value);
        }, delayMs),
      );
    },
    async load(key: string): Promise<string> {
      try {
        const raw = await storage.getItem(PREFIX + key);
        if (!raw) return "";
        const parsed = JSON.parse(raw) as { t?: number; v?: string };
        if (typeof parsed.v !== "string" || typeof parsed.t !== "number" || now() - parsed.t > DRAFT_MAX_AGE_MS) {
          await storage.removeItem(PREFIX + key);
          return "";
        }
        return parsed.v;
      } catch {
        return "";
      }
    },
  };
}

export const draftStore = createDraftStore(AsyncStorage);
